# V2 Remediation Plan

Last reviewed: 2026-03-13

This document converts the v2 review findings into an implementation plan. It defines the recommended decisions, the exact order of work, the files most likely to change, the tests to add, and the acceptance criteria for declaring each issue fixed.

## Guiding decisions

- Safety beats breadth.
- Correctness fixes come before performance work.
- Operator-facing claims must match the actual implementation.
- Incomplete features should fail fast instead of pretending to work.
- Every fix must include tests and operator documentation updates.

## Recommended execution order

## Phase 0: Immediate release containment

### Phase 0 objective

Prevent operators from relying on unsafe or incomplete behavior before deeper fixes land.

### Phase 0 required decisions

- Mark `hybrid` as non-production in docs immediately.
- Mark trigger-based CDC as non-production until durability and key-based matching are fixed.
- Avoid any release notes that imply globally consistent concurrent baseline copy under live writes.

### Phase 0 required changes

- Update CLI/help text and docs:
  - `internal/commands/replicate.go`
  - `README.md`
  - `docs/operators-guide.md`
- If a hard safety gate is preferred, reject:
  - `--replication-mode=hybrid`
  - `--table-routing`
  - or both, until routing enforcement is complete

### Phase 0 tests

- Add CLI validation tests that confirm unsafe feature combinations fail with actionable errors.

### Phase 0 acceptance criteria

- Operators cannot mistake incomplete routing for a supported contract.
- The docs no longer imply stronger guarantees than the code provides.

## Phase 1: Fix CDC durability ordering

### Phase 1 objective

Eliminate the data-loss path caused by purge-before-checkpoint.

### Phase 1 required code changes

- Reorder the CDC run loop in `internal/replicate/cdc/run.go`:
  - apply batch
  - update checkpoint state in memory
  - save checkpoint durably
  - purge source CDC rows after checkpoint save succeeds
- Ensure purge failures are surfaced as cleanup failures, not as correctness failures that force state rollback.
- If purge fails, leave the checkpoint advanced and retry purge on the next run.

### Phase 1 required tests

- Add a test where:
  - batch apply succeeds,
  - purge succeeds,
  - checkpoint save fails
  - expected result: this scenario must no longer be possible after the refactor
- Add a test where:
  - checkpoint save succeeds,
  - purge fails
  - expected result: rerun does not lose data correctness and does not reapply already checkpointed events incorrectly

### Phase 1 suggested implementation hooks

- Use package-level function injection already present in the repository patterns for:
  - checkpoint save
  - purge
  - event read/apply

### Phase 1 acceptance criteria

- No execution order remains that can delete source CDC evidence before durable local state exists.
- Unit tests prove recovery behavior across checkpoint/purge failure permutations.

## Phase 2: Make hybrid routing real or remove it

### Phase 2 objective

Close the contract gap between the CLI/API and the runtime implementation.

### Phase 2 decision

Do not keep partial routing semantics. Choose one of these paths:

- **Preferred**
  - implement real routing enforcement
- **Fallback**
  - remove or hard-fail `hybrid` and `--table-routing` until it can be implemented correctly

### Phase 2 required code changes for the preferred path

- In `internal/replicate/hybrid/run.go`:
  - derive effective CDC and binlog table scopes from `Routing`, `CDCDatabases`, and `BinlogDatabases`
  - reject ambiguous ownership
- In `internal/replicate/cdc/run.go`:
  - add table filtering, not just database filtering
- In `internal/replicate/binlog/load.go` and/or `internal/replicate/binlog/run.go`:
  - filter row events before they become apply batches
  - ensure tables assigned to CDC are never replayed by binlog
- In `internal/commands/replicate.go`:
  - validate that routing definitions are complete, non-conflicting, and explainable to the operator

### Phase 2 required tests

- CDC-routed table is processed only by CDC.
- Binlog-routed table is processed only by binlog.
- Ambiguous route definitions fail at startup.
- Empty routing behaves exactly as documented.
- Cross-database mixes do not accidentally widen scope.

### Phase 2 acceptance criteria

- The runtime behavior matches the published routing contract.
- Every table in scope has exactly one owner.

## Phase 3: Replace CDC full-row matching with key-based matching

### Phase 3 objective

Make trigger-based CDC correct enough to survive normal schema evolution and large tables.

### Phase 3 required code changes

- Add metadata lookup for:
  - primary key columns
  - fallback non-null unique key columns
- Build `UPDATE` and `DELETE` predicates from the stable key only.
- Reject tables without a stable key in trigger-based CDC mode, or treat them as explicitly unsupported.

### Phase 3 files likely to change

- `internal/replicate/cdc/run.go`
- `internal/replicate/cdc/setup.go`
- shared table metadata helpers if one already exists elsewhere in the codebase

### Phase 3 required tests

- Update on a wide table uses only PK columns in the predicate.
- Delete works when non-key columns changed after logging.
- Keyless tables fail fast with a clear remediation message.
- Schema drift that adds destination-only columns does not break matching logic.

### Phase 3 acceptance criteria

- Trigger-based CDC no longer relies on every column remaining identical to match a row.
- The failure mode for keyless tables is explicit and documented.

## Phase 4: Clarify and harden baseline consistency semantics

### Phase 4 objective

Remove the false implication that concurrent live baseline copy is globally consistent.

### Phase 4 decision

Choose one clear product contract:

- **Conservative contract**
  - live-write baselines default to `concurrency=1`
  - concurrent copy is documented as performance-oriented, not globally consistent
- **Stronger contract**
  - only if a true global snapshot strategy is implemented and tested

### Phase 4 required code and doc changes for the conservative contract

- In `internal/commands/migrate.go` and `internal/data/copy.go`:
  - warn when live baseline runs with `concurrency > 1`
  - optionally auto-reduce concurrency unless an explicit override flag is set
- In operator docs:
  - document that per-table snapshots are not equivalent to a database-wide snapshot

### Phase 4 required tests

- Validation or warning path for live baseline with `concurrency > 1`
- Updated operator docs checked into the repo

### Phase 4 acceptance criteria

- The tool no longer overstates its consistency guarantees.

## Phase 5: Fix replication identity handling

### Phase 5 objective

Avoid `server_id` collisions between independent operators.

### Phase 5 required code changes

- Prefer an explicit `--source-server-id`.
- If absent, generate a random `server_id` once and persist it under `state-dir`.
- Reuse the persisted value on resume.
- Validate that the generated ID is within the legal MySQL range and not zero.

### Phase 5 files likely to change

- `internal/replicate/binlog/load.go`
- `internal/state/`
- `internal/commands/replicate.go`

### Phase 5 required tests

- Same `state-dir` reuses the same generated ID.
- Different `state-dir` values generate different IDs.
- Explicit CLI value overrides the persisted value.

### Phase 5 acceptance criteria

- Replication identity is stable per run-state and avoids DSN-based collisions.

## Phase 6: Fix the first scalability bottlenecks

### Phase 6 objective

Address the parts that will break first at higher throughput.

### Phase 6 work item A: Stream binlog apply incrementally

- Replace whole-window buffering with bounded transaction streaming.
- Checkpoint after each committed transaction or bounded group.
- Preserve DDL boundary handling and conflict-report semantics.

### Phase 6 work item B: Batch baseline inserts

- Replace row-by-row `ExecContext` with multi-row inserts or prepared statement reuse.
- Keep parameterization and identifier quoting intact.

### Phase 6 work item C: Replace expensive existence checks

- Change destination emptiness probes from `COUNT(*)` to `SELECT 1 ... LIMIT 1`.

### Phase 6 work item D: Reduce checkpoint write contention

- Persist per-table checkpoint files or flush checkpoints on a timer/batch interval.

### Phase 6 required tests

- Unit coverage for generated multi-row SQL.
- Resume behavior still works after batched checkpoint flushing.
- Large backlog replay uses bounded memory in tests or instrumentation.

### Phase 6 acceptance criteria

- The dominant scale bottlenecks move from architectural hard limits to tunable limits.

## Phase 7: Security hardening

### Phase 7 objective

Lower the blast radius of expected operator mistakes and high-sensitivity workloads.

### Phase 7 work item A: Reduce CDC payload sensitivity

- Revisit whether full row images are necessary.
- Prefer:
  - key columns
  - changed columns
  - minimal metadata for apply
- Publish retention and privilege requirements for CDC tables.

### Phase 7 work item B: Harden conflict-report output

- Keep redaction as default.
- Gate plaintext output behind an explicitly unsafe flag or confirmation pair.
- Ensure all report-writing paths continue to use private permissions.

### Phase 7 work item C: Tighten transport guidance

- Warn when `tls-mode=preferred` is used in replication.
- Document `required` as the production-safe minimum.

### Phase 7 work item D: Add privilege prechecks

- Publish least-privilege account requirements for:
  - plan
  - migrate
  - replicate
  - trigger setup/teardown
- Fail early if obvious required privileges are missing.

### Phase 7 required tests

- Redaction remains enabled by default.
- Unsafe plaintext mode requires the stronger opt-in path.
- Privilege precheck failures produce actionable messages.

### Phase 7 acceptance criteria

- Security-sensitive behavior is safe by default and risky modes are intentionally explicit.

## Suggested branch plan

To keep the work reviewable, use one branch per major fix:

- `fix/cdc-durability-order`
- `fix/hybrid-routing-enforcement`
- `fix/cdc-keyed-apply`
- `fix/baseline-consistency-contract`
- `fix/binlog-server-id-persistence`
- `fix/binlog-streaming-apply`
- `fix/baseline-batch-apply`
- `fix/security-hardening-replication`

## Validation matrix

Every implementation branch should run at least:

- `go test ./... -count=1`
- `go vet ./...`

For changes touching replication semantics, also add or expand:

- unit tests for injected failure ordering
- unit tests for scope/routing enforcement
- smoke tests for restart/resume behavior
- operator doc updates in the same branch

## Exit criteria for calling v2 ready

Do not call v2 ready until all of the following are true:

- CDC durability ordering is fixed and tested.
- Hybrid routing is either fully enforced or disabled.
- Trigger-based CDC uses stable-key matching or explicitly rejects unsupported tables.
- Baseline consistency claims are accurate in both CLI and docs.
- Replication identity collisions are prevented.
- Conflict reporting and transport guidance are safe by default.
- Tests and docs reflect the actual shipped contract.
