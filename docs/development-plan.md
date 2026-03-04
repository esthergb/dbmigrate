# dbmigrate Development Plan

## Delivery model

- Work in small PR-sized milestones.
- One feature/fix/chore per branch.
- PR target is `main`.
- CI must be green before merge.

## Milestone sequence and deliverables

### 1) Research docs and operator risk baseline

Objective:
- Document real migration/replication failure modes and mitigations.

Files/modules:
- `docs/known-problems.md`
- `docs/risk-checklist.md`

Tests:
- Documentation review for source coverage and mitigation mapping.

Acceptance criteria:
- All required problem domains covered with linked references and safeguards.

### 2) Repository scaffolding and CI baseline

Objective:
- Establish Go project structure, command skeleton, quality gates, contribution workflow.

Files/modules:
- `cmd/dbmigrate/`
- `internal/cli`, `internal/config`, `internal/commands`, `internal/version`
- `.github/workflows/ci.yml`
- `.github/pull_request_template.md`
- `CONTRIBUTING.md`, `README.md`, `LICENSE`

Tests:
- `go test ./... -count=1`
- CI format/lint/test/vuln checks.

Acceptance criteria:
- CLI binary builds and subcommands execute scaffold behavior.
- CI validates formatting, tests, lint and vulnerability checks.

### 3) Config system and connection layer

Objective:
- Add validated config file support and DSN credential-safe handling.

Files/modules:
- `internal/config`
- `internal/db`
- `cmd/dbmigrate` wiring

Tests:
- Config parsing and precedence tests.
- Connection validation tests using mocks/fakes.

Acceptance criteria:
- CLI resolves flags + config file with deterministic precedence.

### 4) Extractor and schema applier baseline

Objective:
- Extract source schema and apply compatible schema on destination.

Files/modules:
- `internal/extractor`
- `internal/transform`
- `internal/applier`

Tests:
- Unit tests for DDL normalization and compatibility checks.
- Integration tests for same-engine baseline schema migration.

Acceptance criteria:
- Schema-only migration works for first priority path (MariaDB->MariaDB).

### 5) Data migrator streaming with checkpoint/resume

Objective:
- Migrate baseline data in chunks with resumable checkpoints.

Files/modules:
- `internal/migrate`
- `internal/state`

Tests:
- Chunking and resume crash-safety tests.
- Integration tests with deterministic seeded data.

Acceptance criteria:
- Data-only and full baseline migration supports restart without duplication.

### 6) Schema verifier

Objective:
- Compare schema objects with normalization and ignore controls.

Files/modules:
- `internal/verify/schema`

Tests:
- Unit tests for diff normalization.
- Integration tests with intentional schema mismatches.

Acceptance criteria:
- `verify --verify-level=schema` produces actionable diffs and exit codes.

### 7) Data verifier (count/hash/sample/full-hash)

Objective:
- Implement deterministic data consistency checks.

Files/modules:
- `internal/verify/data`

Tests:
- Hash serialization tests.
- Integration mismatch detection tests.

Acceptance criteria:
- All verification modes are selectable and deterministic.

### 8) Binlog replicator with checkpoints

Objective:
- Implement incremental replication with transaction boundaries and idempotent checkpointing.

Files/modules:
- `internal/replicate/binlog`
- `internal/state`

Tests:
- Unit tests for event apply ordering/checkpoint semantics.
- Integration tests with repeated replicate runs and crash simulation.

Acceptance criteria:
- Repeated replicate runs apply only new changes since last successful checkpoint.

### 9) DDL handling and conflict detection

Objective:
- Apply policy-gated DDL during replication and produce conflict reports.

Files/modules:
- `internal/replicate/ddl`
- `internal/report`

Tests:
- Conflict policy tests (`fail` default).
- Risky DDL handling tests across `--apply-ddl` modes.

Acceptance criteria:
- Risky DDL is blocked or applied according to explicit policy.

### 10) Trigger CDC fallback and teardown

Objective:
- Implement fallback CDC for environments where binlog mode is unavailable.

Files/modules:
- `internal/replicate/cdc`

Tests:
- CDC setup/apply/teardown integration tests.

Acceptance criteria:
- CDC mode works only when explicitly enabled and cleans up safely.

### 11) Report output and UX polish

Objective:
- Emit structured JSON report with diffs, warnings, transforms, checkpoints.

Files/modules:
- `internal/report`
- `cmd/dbmigrate`

Tests:
- JSON schema and regression snapshot tests.

Acceptance criteria:
- Report is machine-readable and complete for automation.

### 12) Documentation hardening and release readiness

Objective:
- Finalize runbooks, security docs, examples, limitations, release process.

Files/modules:
- `docs/operators-guide.md`
- `docs/security.md`
- `README.md`

Tests:
- Full local Docker matrix and release checklist.

Acceptance criteria:
- Operators can run baseline + incremental workflow safely from docs.

## Branching and PR execution rule (explicit)

Every task above must be delivered as one or more small branches with PRs to `main`, each with:
- clear objective,
- test evidence,
- compatibility notes,
- green CI.
