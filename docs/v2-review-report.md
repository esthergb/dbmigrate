# V2 Review Report and Release Decision

Last reviewed: 2026-03-13

This document consolidates the senior engineering review and security audit for the `dbmigrate` v2 workstream, with special focus on baseline copy, trigger-based CDC, hybrid replication, GTID resume, state persistence, and operator-facing safety controls.

## Executive summary

The v2 codebase contains useful functionality, but the current release posture is uneven:

- `GTID` is the least risky v2 addition and can become release-ready after focused validation.
- `capture-triggers` is not yet durable enough for production because the checkpoint and purge ordering can lose data and the row-matching strategy is too brittle.
- `hybrid` is not production-ready because its routing contract is not actually enforced at apply time.

The highest-risk defects are correctness defects, not style defects. The main concern is not that the system crashes visibly; it is that it can continue while giving operators a false sense of safety.

## Release decision

### Ship / do not ship matrix

- **Ship with focused validation**
  - `--start-from=gtid`

- **Do not ship as production-ready**
  - `--replication-mode=capture-triggers`
  - `--replication-mode=hybrid`
  - `--table-routing`

### Required release gates before any v2 replication announcement

- Fix CDC durability ordering.
- Enforce hybrid routing contract in both CDC and binlog paths.
- Replace CDC full-row matching with key-based matching.
- Clarify baseline consistency guarantees in docs and CLI behavior.
- Harden the default security posture for conflict-value output and transport security guidance.

## Findings

## 1) Critical findings

### 1.1 CDC can lose data if checkpoint save fails

- **Severity**
  - Critical
- **Location**
  - `internal/replicate/cdc/run.go:199-223`
- **What is wrong**
  - The current order is: apply events, purge CDC rows from the source, then save the local checkpoint.
  - If the checkpoint save fails after purge succeeds, the source evidence is gone and the destination checkpoint is stale.
- **Impact**
  - Permanent drift and unrecoverable data loss after a local disk/state failure.
- **Decision**
  - Treat checkpoint persistence as part of the correctness path and treat purge as cleanup only.
- **Fix summary**
  - Save checkpoint first.
  - Purge only after the checkpoint has been durably written.
  - Make purge retriable and non-authoritative for correctness.

### 1.2 Hybrid table routing is not implemented end-to-end

- **Severity**
  - Critical
- **Location**
  - `internal/replicate/hybrid/run.go:59-127`
  - `internal/commands/replicate.go:207-238`
- **What is wrong**
  - Routing is parsed, but the runtime path does not enforce table-level routing in the CDC or binlog apply flows.
- **Impact**
  - The feature contract is misleading. Operators can believe specific tables are routed one way while the engine does something else.
- **Decision**
  - `hybrid` must be treated as incomplete until routing is enforced in the actual apply pipeline.
- **Fix summary**
  - Add explicit include/exclude enforcement to both phases and reject invalid or ambiguous routing at startup.

### 1.3 Hybrid binlog replay can duplicate CDC-routed changes

- **Severity**
  - Critical
- **Location**
  - `internal/replicate/hybrid/run.go:107-127`
  - `internal/replicate/binlog/load.go:300-585`
- **What is wrong**
  - The hybrid binlog phase is launched without a matching table filter. It can replay changes that were already handled by CDC.
- **Impact**
  - Duplicate writes, artificial conflicts, divergent outcomes, and operator confusion.
- **Decision**
  - Prevent dual ownership of the same table between CDC and binlog.
- **Fix summary**
  - Introduce table-aware filtering for binlog event conversion/apply and reject any startup configuration that cannot prove exclusivity.

## 2) High findings

### 2.1 Baseline copy is not a globally consistent snapshot

- **Severity**
  - High
- **Location**
  - `internal/data/copy.go:71-75`
  - `internal/data/copy.go:97-107`
  - `internal/data/copy.go:165-166`
  - `internal/data/copy.go:246-260`
- **What is wrong**
  - Each worker opens its own consistent snapshot transaction, which means the overall baseline can observe different tables at different source times.
- **Impact**
  - Cross-table inconsistencies under live writes. Parent/child relations can reflect different moments.
- **Decision**
  - Do not claim global baseline consistency for concurrent live-copy mode.
- **Fix summary**
  - Short term: force or strongly recommend `concurrency=1` for live sources.
  - Medium term: implement or require a true global snapshot strategy.

### 2.2 Derived replication `server_id` can collide across operators

- **Severity**
  - High
- **Location**
  - `internal/replicate/binlog/load.go:937-989`
- **What is wrong**
  - The default `server_id` is derived from `user@addr`.
- **Impact**
  - Two operators using the same DSN on different machines can get the same `server_id` and interfere with each other.
- **Decision**
  - A replication identity must be stable and unique per run-state, not just per DSN.
- **Fix summary**
  - Require explicit `--source-server-id` or persist a generated random stable ID in `state-dir`.

### 2.3 Binlog apply buffers too much in memory

- **Severity**
  - High
- **Location**
  - `internal/replicate/binlog/load.go:84-204`
- **What is wrong**
  - The replay window is buffered before apply rather than streamed incrementally.
- **Impact**
  - Memory pressure becomes the first major scale failure mode.
- **Decision**
  - Move toward streaming apply with incremental checkpointing.
- **Fix summary**
  - Apply transactions incrementally and checkpoint per transaction or bounded group.

### 2.4 CDC `UPDATE` and `DELETE` use all columns in the `WHERE` clause

- **Severity**
  - High
- **Location**
  - `internal/replicate/cdc/run.go:315-362`
- **What is wrong**
  - The row-matching predicate is built from every column instead of a stable key.
- **Impact**
  - Slow matching, fragile behavior under schema drift, and false misses on wide tables.
- **Decision**
  - Key-based matching is mandatory for production CDC apply.
- **Fix summary**
  - Resolve primary key or non-null unique key metadata and use only those columns in match predicates.

## 3) Medium findings

### 3.1 CDC only drains one batch per schema per loop

- **Severity**
  - Medium
- **Location**
  - `internal/replicate/cdc/run.go:180-223`
- **What is wrong**
  - The code processes one batch and moves on instead of draining until empty or until an explicit bound is reached.
- **Impact**
  - A busy schema can stay permanently behind.
- **Decision**
  - Drain behavior must be explicit and configurable, not accidental.

### 3.2 Destination emptiness checks use `COUNT(*)`

- **Severity**
  - Medium
- **Location**
  - `internal/data/copy.go:827-840`
- **What is wrong**
  - `COUNT(*)` is used to answer a boolean question.
- **Impact**
  - Unnecessary full scans on large tables.
- **Decision**
  - Use existence checks instead of full counts.

### 3.3 Baseline apply is row-by-row

- **Severity**
  - Medium
- **Location**
  - `internal/data/copy.go:761-777`
- **What is wrong**
  - Each row is applied with its own `ExecContext`.
- **Impact**
  - Poor throughput and excess round trips.
- **Decision**
  - Batch inserts should be the default implementation path.

### 3.4 Checkpoint persistence serializes worker progress

- **Severity**
  - Medium
- **Location**
  - `internal/data/copy.go:348-355`
  - `internal/data/copy.go:364-370`
- **What is wrong**
  - Workers contend on a single mutex and rewrite the whole checkpoint file frequently.
- **Impact**
  - Contention and excessive disk churn reduce the value of concurrency.
- **Decision**
  - Move to per-table or timed checkpoint flushing.

## 4) Low findings

### 4.1 `Restarted` reporting looks incomplete

- **Severity**
  - Low
- **Location**
  - `internal/data/copy.go:33-44`
  - `internal/commands/migrate.go:302-317`
- **What is wrong**
  - The summary field exists, but it does not appear to be meaningfully maintained.
- **Impact**
  - Misleading operator output.
- **Decision**
  - Either implement accurate reporting or remove the field from human-facing summaries.

## Security findings

### 5.1 Trigger CDC stores full row images in source tables

- **Severity**
  - High
- **Location**
  - `internal/replicate/cdc/setup.go:48-57`
  - `internal/replicate/cdc/setup.go:60-99`
- **Attack scenario**
  - Anyone with read access to the source database, replica, backup, or support artifact can extract sensitive row data from `old_row_json` and `new_row_json`.
- **Decision**
  - Full row images are too expensive and too sensitive as the default CDC payload.
- **Fix summary**
  - Store keys plus changed columns where possible.
  - Tighten privileges and retention windows.
  - Document data-classification risk explicitly.
- **References**
  - `CWE-200`
  - `CWE-359`
  - `CWE-532`

### 5.2 Plaintext conflict reports can persist sensitive samples

- **Severity**
  - Medium
- **Location**
  - `internal/replicate/binlog/run.go:869-919`
  - `internal/state/replication_conflict.go:10-35`
- **Attack scenario**
  - An operator enables `--conflict-values=plain`; the generated JSON report is later copied to tickets, backups, or CI artifacts.
- **Decision**
  - Plaintext conflict values should be an exceptional diagnostic mode, not a casual option.
- **Fix summary**
  - Keep redaction as the safe default.
  - Rename plain mode as unsafe or guard it with an additional explicit acknowledgment flag.
- **References**
  - `CWE-200`
  - `CWE-532`

### 5.3 `tls-mode=preferred` allows transport downgrade

- **Severity**
  - Medium
- **Location**
  - `internal/db/connector.go:190-235`
- **Attack scenario**
  - On an untrusted network, the operator believes traffic is protected while the session can silently fall back to plaintext.
- **Decision**
  - Production guidance must prefer `required` and treat `preferred` as non-hardened.
- **Fix summary**
  - Warn or reject `preferred` in production-facing replication modes and document the risk clearly.
- **References**
  - `CWE-319`

### 5.4 Privilege boundaries are too implicit for destructive paths

- **Severity**
  - Medium
- **Location**
  - `internal/replicate/cdc/setup.go:141-185`
  - `internal/replicate/binlog/run.go:806-842`
- **Attack scenario**
  - A mis-scoped or stolen DSN can create triggers, drop CDC structures, or mutate checkpoint tables without any privilege preflight gate.
- **Decision**
  - The tool must document and check least-privilege requirements per command family.
- **Fix summary**
  - Add explicit privilege prechecks and publish least-privilege role examples.
- **References**
  - `CWE-250`

## Strategic assessment

### Top three risks

- **Silent correctness drift**
  - Hybrid routing and concurrent baseline semantics can violate operator expectations without obvious failure.

- **Durability bugs in CDC state handling**
  - Purge-before-checkpoint can permanently lose the replay source of truth.

- **Scale failure due to architecture**
  - Full-window buffering, row-by-row inserts, and whole-file checkpoints will degrade sharply at larger data volumes.

### What breaks first at 10x scale

- **Binlog replay memory pressure**
  - The buffered replay window is the earliest hard wall.

- **Baseline throughput**
  - Row-at-a-time destination writes will dominate runtime.

- **Trigger-CDC source overhead**
  - JSON row logging increases write amplification and retention costs.

### Simplest version to ship first

- Baseline migration with conservative defaults.
- Binlog incremental replication only.
- No `hybrid`.
- No production claim for trigger-based CDC.
- Redacted conflict reporting only.

### Alternatives worth considering

- Use external snapshot/backup tooling for baseline and keep `dbmigrate` focused on planning, verification, and replay.
- Keep binlog replication as the primary incremental path and reframe trigger CDC as a fallback/experimental mode.
- Make hybrid opt-in only after routing can be proven correct at startup.

### Time-based tradeoffs

- **With less time**
  - Disable or fail fast on incomplete features.
  - Fix correctness bugs before performance work.

- **With more time**
  - Redesign binlog apply as a streaming pipeline.
  - Rework CDC around stable keys and lower-sensitivity payloads.
  - Define a real consistency contract for live baselines.
