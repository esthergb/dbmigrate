# Executive Summary

- **Verdict**: against the broader production spec in this review, `dbmigrate` is **not yet production-ready** for general live MySQL/MariaDB migration with incremental replication and full verification.
- **Strongest areas**: fail-fast compatibility prechecks, clear exit codes, atomic state artifacts, disciplined docs, and a reasonably safe replication transaction model (`internal/commands/exit_error.go:5-10`, `internal/state/checkpoint.go:57-85`, `internal/replicate/binlog/run.go:158-187`).
- **Main blocker**: baseline data copy is not snapshot-consistent and paginates with unordered `LIMIT/OFFSET`, so hot-table baseline correctness is not trustworthy (`internal/data/copy.go:130-157`, `internal/data/copy.go:409-416`).
- **Main scope gap**: migrate/verify only cover **tables/views**, while defaults imply `routines,triggers,events` too (`internal/config/runtime.go:60`, `internal/commands/migrate.go:152-160`, `internal/schema/copy.go:231-270`, `internal/verify/schema/verify.go:132-177`).
- **Main security gap**: TLS flags are parsed but not wired through the SQL connection layer, and binlog sync rejects most real TLS modes (`internal/config/runtime.go:66-69`, `internal/db/connector.go:65-89`, `internal/replicate/binlog/load.go:767-769`).
- **Main scalability gap**: verify `sample` still full-scans, and `full-hash` is only an alias for `hash` (`internal/verify/data/verify.go:214-216`, `internal/verify/data/verify.go:451-508`, `internal/verify/data/verify.go:495-498`).
- **Incremental replication**: better than baseline migration, but still below spec because GTID is unimplemented, `--idempotent` is effectively a no-op, and keyless table replay is unsafe (`internal/commands/replicate.go:100-109`, `internal/replicate/binlog/run.go:17-28`, `internal/replicate/binlog/load.go:542-547`, `internal/replicate/binlog/load.go:637-685`).
- **Testing/CI**: `go test ./... -count=1` passes, but CI does not run Docker integration, and the main script does not prove mutate -> `replicate` -> `verify full` or crash/restart behavior (`.github/workflows/ci.yml:13-50`, `scripts/run-migration-test.sh:130-168`).
- **Bottom line**: acceptable only for a **narrow, heavily rehearsed, frozen support matrix**. Not acceptable yet as a broad production migration utility under the stated threat model.

## High-Risk Findings

### 1) Baseline copy is not consistent and is nondeterministic on live tables

- **Type**: Bug + design flaw
- **Impact**: silent skip/duplication/reordering of baseline rows.
- **Likelihood**: High
- **Evidence**:
  - `internal/data/copy.go:130-146` — `"offset := int64(0)"`, `"offset += int64(len(batch))"`
  - `internal/data/copy.go:409-416` — `"SELECT %s FROM %s.%s LIMIT ? OFFSET ?"`
  - Repo search found no `REPEATABLE READ`, `LOCK TABLES`, or `START TRANSACTION WITH CONSISTENT SNAPSHOT` in `internal/`.
- **Fix**: add snapshot-consistent source reads, keyset pagination by stable key, and a baseline handoff watermark.

## 2) Baseline `--resume` is destructive restart, not true resume

- **Type**: Design flaw
- **Impact**: large-table restarts, lock amplification, trigger/audit side effects.
- **Likelihood**: High
- **Evidence**:
  - `internal/data/copy.go:101-113` — `"resetDestinationTable(...)"`
  - `internal/data/copy.go:559-569` — `"TRUNCATE TABLE"` with `"DELETE FROM"` fallback
  - `docs/operators-guide.md:79-82` — `"restarts incomplete tables safely (truncate/delete fallback)"`
- **Fix**: checkpoint by last committed key/chunk, keep restart behavior behind an explicit destructive flag.

### 3) Schema migrate/verify ignore routines, triggers, events, and account/grant migration

- **Type**: Design flaw
- **Impact**: successful runs can still yield functionally incomplete destinations.
- **Likelihood**: High
- **Evidence**:
  - `internal/config/runtime.go:60` — default `"tables,views,routines,triggers,events"`
  - `internal/commands/migrate.go:152-160` — schema path only uses `IncludeTables` / `IncludeViews`
  - `internal/schema/copy.go:245-266` — only `SHOW CREATE TABLE` / `SHOW CREATE VIEW`
  - `internal/verify/schema/verify.go:135-177` — verifier only lists tables/views
  - `docs/operators-guide.md:49-49` — baseline migration `"does not recreate accounts yet"`
- **Fix**: either fail fast on unsupported object types or implement routines/triggers/events plus explicit account/grant workflows.

## 4) TLS surface is misleading and mostly nonfunctional

- **Type**: Bug + security flaw
- **Impact**: operators may think TLS is enforced when it is not.
- **Likelihood**: High
- **Evidence**:
  - `internal/config/runtime.go:66-69` defines TLS flags
  - `internal/db/connector.go:65-89` accepts only raw DSN; no runtime TLS inputs
  - `internal/commands/replicate.go:84-100` passes only `cfg.Source` / `cfg.Dest` into `OpenAndPing`
  - `internal/replicate/binlog/load.go:767-769` — `"unsupported source tls mode ... for binlog sync"`
- **Fix**: centralize TLS config building for both SQL and binlog connections; add CA/mTLS tests.

### 5) Verification modes are not scalable or semantically distinct enough

- **Type**: Design flaw
- **Impact**: high memory/IO cost on large tables; misleading mode names.
- **Likelihood**: High
- **Evidence**:
  - `internal/verify/data/verify.go:214-216` — `full-hash` calls `VerifyHash`
  - `internal/verify/data/verify.go:451-508` — row hashes are accumulated and sorted in memory
  - `internal/verify/data/verify.go:495-498` — sample limit applied only after full scan/sort
- **Fix**: make `hash`, `sample`, and `full-hash` genuinely distinct; use bounded-memory chunked aggregation.

## 6) Replication idempotency is not implemented, and keyless replay is unsafe

- **Type**: Bug + design flaw
- **Impact**: weaker crash/replay guarantees than the CLI suggests; ambiguous update/delete targeting.
- **Likelihood**: Medium-high
- **Evidence**:
  - `internal/commands/replicate.go:100-109` passes `Idempotent`
  - `internal/replicate/binlog/run.go:17-28` only declares `Idempotent bool`
  - grep across `internal/replicate` shows no operational use of `Idempotent`
  - `internal/replicate/binlog/load.go:542-547` falls back to all-column matching when no PK exists
  - `internal/replicate/binlog/load.go:637-685` only inventories `PRIMARY KEY`
- **Fix**: reject `--idempotent` until real, prefer PK or unique key identity, and block keyless update/delete replay by default.

### 7) Conflict artifacts can leak sensitive data

- **Type**: Security flaw
- **Impact**: PII/secrets can be written to disk during conflict handling.
- **Likelihood**: High if conflicts occur on sensitive tables.
- **Evidence**:
  - `internal/replicate/binlog/failure.go:123-210` builds `value_sample`, `old_row_sample`, `new_row_sample`, `row_diff_sample`
  - `internal/state/replication_conflict.go:11-35` persists those fields
  - `internal/state/replication_conflict.go:64-87` writes the artifact to disk
- **Fix**: default to redacted output with explicit opt-in for plain values.

## Correctness & Data Safety

- **Good**:
  - safe defaults for destination emptiness and replication conflict policy (`internal/commands/migrate.go:206-218`, `internal/commands/replicate.go:150-159`)
  - atomic checkpoint/report writes (`internal/state/checkpoint.go:57-85`, `internal/state/replication_conflict.go:64-87`)
  - useful fail-fast prechecks for zero dates, collations, plugins, and invisible/GIPK drift.
- **Weak**:
  - baseline correctness on hot systems is not reliable because the copy path is neither snapshot-based nor key-ordered.
  - baseline/incremental handoff is not modeled as one atomic correctness story.
  - schema replay is mostly raw `SHOW CREATE` apply, not a transformation layer (`internal/schema/copy.go:245-266`, `internal/schema/copy.go:465-469`).
- **Safety judgment**: strong intent, but the baseline data plane is still below production bar.

## Incremental Replication Review

- **What is good**:
  - transaction boundaries are preserved (`internal/replicate/binlog/load.go:213-345`)
  - checkpoint advances only after successful apply/save (`internal/replicate/binlog/run.go:158-187`)
  - DDL policy defaults are conservative and the risky-DDL classifier blocks destructive patterns (`internal/replicate/binlog/load.go:253-299`, `internal/replicate/binlog/load.go:730-739`)
  - failure classification and remediation are better than average (`internal/replicate/binlog/failure.go:47-112`).
- **What is missing**:
  - GTID is explicitly unsupported (`internal/commands/replicate.go:68-72`)
  - `--idempotent` is not real
  - keyless update/delete replay is unsafe
  - `apply-ddl=ignore` can defer drift into later apply failures.
- **Judgment**: replication is the strongest subsystem, but still below the requested spec.

## Verification Review

- **Strengths**:
  - good canonicalization of JSON/time zone/session settings (`internal/verify/data/verify.go:569-580`, `internal/verify/data/verify.go:691-696`)
  - clear representation-risk reporting (`internal/verify/data/verify.go:699-794`)
  - schema normalization strips definers and volatile `AUTO_INCREMENT` noise (`internal/verify/schema/verify.go:299-307`).
- **Weaknesses**:
  - schema verify covers only tables/views (`internal/verify/schema/verify.go:132-177`)
  - `normalizeCreateStatement` lowercases everything, so case-only drift disappears (`internal/verify/schema/verify.go:305-306`)
  - `maybeJSONValue` canonicalizes JSON-looking text too, which can hide formatting differences in plain text columns (`internal/verify/data/verify.go:796-811`).
- **Judgment**: good core ideas, insufficient coverage and scalability.

## Compatibility & Transformations

- **Strong**: research-backed prechecks and docs are excellent (`docs/risk-checklist.md`, `docs/known-problems.md`, `docs/operators-guide.md`).
- **Current reality**:
  - the product is mostly a **fail-fast compatibility checker + replay tool**, not a rich transformation engine.
  - collations, auth plugins, SQL mode, invisible/GIPK, and version caveats are mostly **reported**, not automatically transformed.
  - account/grant state is inventory-only today (`docs/operators-guide.md:46-51`, `internal/commands/plugin_precheck.go:197-203`, `internal/commands/plugin_precheck.go:459-470`).
- **Judgment**: compatibility research is ahead of compatibility execution.

## Performance & Scalability

- **Baseline copy**: single-threaded and row-at-a-time insert inside a transaction (`internal/data/copy.go:474-489`).
- **Concurrency**: `--concurrency` is parsed but I did not find a data-plane consumer (`internal/config/runtime.go:61`).
- **Replication**: safely serialized by source transaction, but no parallel apply.
- **Verification**: biggest scalability issue; full-table hash materialization is not viable for very large tables.
- **Judgment**: not yet engineered for hundreds of GB / sustained high write throughput.

## Reliability & Observability

- **Good**:
  - atomic artifacts
  - useful `report` command and replication conflict details
  - transaction-shape telemetry is practical.
- **Missing**:
  - little streaming progress for long runs (`internal/commands/migrate.go:188-202`, `internal/commands/verify.go:330-387`)
  - no metrics surface
  - no built-in retry/backoff even for classified retryable failures (`internal/replicate/binlog/failure.go:95-100`)
  - connection timeouts are shallow beyond initial ping (`internal/db/connector.go:77-86`).
- **Judgment**: decent artifact observability, limited runtime observability.

## Security Review

- **TLS**: primary blocker; flags exist but are not enforced end-to-end.
- **Sensitive data**: replication conflict artifacts can persist raw row values.
- **Credential redaction**:
  - `db.RedactDSN` exists (`internal/db/connector.go:38-63`) but grep only found it in tests, not production paths.
- **Least privilege**: documentation is good (`docs/security.md:5-19`), but enforcement is mostly reactive via permission errors.
- **Judgment**: staging-grade, not broad production-grade.

## Testing Review

- **Observed now**: `go test ./... -count=1` passed locally.
- **Good**: unit coverage around parsing, normalization, conflict reporting, and exit codes is healthy.
- **Gaps**:
  - CI does not run Docker integration (`.github/workflows/ci.yml:13-50`)
  - main harness does not prove mutate -> `replicate` -> `verify full` (`scripts/run-migration-test.sh:130-168`)
  - I found no crash/restart integration proof for baseline resume or replication replay safety
  - no `run_smoke.sh` / `run_full_matrix.sh` files were present
  - `scripts/run-migration-test.sh:123-128` can reuse a stale `bin/dbmigrate` locally.
- **Judgment**: good unit coverage, insufficient behavioral assurance.

## CI/CD & Release Review

- **Good**: `gofmt`, `go test`, `golangci-lint`, `govulncheck` are in CI (`.github/workflows/ci.yml:13-50`).
- **Missing**:
  - integration tests
  - release artifact build validation
  - checksums
  - SBOM
  - signing / provenance.
- **Reproducibility issues**:
  - CI uses `go-version: stable` instead of pinning the repo toolchain (`go.mod:3`, `.github/workflows/ci.yml:20-24`)
  - lint action uses `version: latest` (`.github/workflows/ci.yml:41-45`)
  - Docker images are tag-pinned, not digest-pinned (`docker-compose.yml:1-268`).
- **Dependency posture**:
  - direct deps are minimal and reasonable (`go.mod:5-9`)
  - transitive weight is non-trivial (`go.mod:11-27`), mainly from the binlog stack.
- **Judgment**: good guardrails, not yet release-grade packaging.

## API/CLI UX Review

- **Good**: clear subcommands, useful `report`, explicit exit codes, good fail-fast messages for reserved modes.
- **Misleading today**:
  - `--include-objects` default overstates support
  - `--concurrency` is dead
  - `--idempotent` is dead
  - TLS flags are dead/nonfunctional
  - `sample` / `full-hash` names oversell distinct behavior.
- **Judgment**: well-structured CLI, but several flags should be removed, made real, or made fail-fast.

## Documentation Review

- **Strong**: `docs/known-problems.md`, `docs/risk-checklist.md`, `docs/operators-guide.md`, and release-signoff docs are thoughtful and useful.
- **Drift from code**:
  - `README.md:197-199` says `--idempotent` `"enables replay-safety guardrails"`, but engine behavior does not change.
  - `docs/security.md:11-12` recommends `--tls-mode=required`, but the runtime path does not honor those flags.
  - `docs/operators-guide.md:79-82` calls restart-based resume `"safely"`, which is operationally optimistic.
  - docs are not explicit enough that schema migrate/verify are tables/views only despite the wider default object list.
- **Judgment**: documentation quality is high, but several promises must be tightened to match reality.

## Recommended Refactor Plan

1. **Support-surface truthfulness**
   - fail fast on unsupported object types and no-op flags
   - narrow defaults and docs.
2. **TLS / connection hardening**
   - one shared connection builder for SQL + binlog
   - CA/mTLS tests.
3. **Baseline correctness**
   - snapshot reads
   - keyset chunking
   - last-key checkpoints
   - baseline handoff watermark.
4. **Replication safety**
   - real idempotency or explicit rejection
   - stable unique-key identity
   - block keyless update/delete replay by default.
5. **Verification scalability**
   - distinct `hash` / `sample` / `full-hash`
   - bounded-memory hashing
   - richer schema coverage.
6. **Release hardening**
   - Docker integration in CI
   - end-to-end replicate tests
   - checksums/SBOM/provenance.

## Quick Wins

- **Fail on unsupported object types by default** — `internal/config/runtime.go`, `internal/commands/migrate.go`, `internal/commands/verify.go`, `README.md`
- **Reject `--idempotent` until implemented** — `internal/commands/replicate.go`, `README.md`
- **Wire TLS flags into real connection setup** — `internal/db/connector.go`, `internal/replicate/binlog/load.go`, `internal/db/connector_test.go`
- **Redact conflict values by default** — `internal/replicate/binlog/failure.go`, `internal/state/replication_conflict.go`, `docs/security.md`
- **Split `hash` / `sample` / `full-hash` semantics** — `internal/verify/data/verify.go`, `internal/commands/verify.go`, `README.md`
- **Document hot-baseline limitation explicitly** — `README.md`, `docs/operators-guide.md`
- **Add one CI integration path** — `.github/workflows/ci.yml`, `scripts/run-migration-test.sh`
- **Always rebuild the local test binary in the harness** — `scripts/run-migration-test.sh`
