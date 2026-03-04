# dbmigrate execution phases

Use this as the default milestone map.

## Phase 0: Mandatory Research and Risk Artifacts

- Produce `docs/known-problems.md` with source links and mitigations.
- Produce `docs/risk-checklist.md` for operators.
- Block implementation until both documents exist and are review-ready.

## Phase 1: Repository Foundation

- Initialize Go module and CLI skeleton.
- Add lint/format/test scripts and minimal CI workflow.
- Add contribution workflow and PR template.

## Phase 2: Connectivity and Plan Pipeline

- Implement config loading/validation and secure connection handling.
- Implement `plan` command with compatibility warnings.

## Phase 3: Baseline Migration

- Implement schema extraction/apply and baseline data migration.
- Add checkpoint/resume primitives reused later by replication.

## Phase 4: Verification Engine

- Implement schema verification.
- Implement data verification modes: count/hash/sample/full-hash.

## Phase 5: Incremental Replication

- Implement binlog mode with transaction boundaries and checkpoints.
- Implement DDL handling via `--apply-ddl` policy.
- Implement conflict reports and default `fail` behavior.

## Phase 6: CDC Fallback and Hybrid

- Implement trigger CDC mode with explicit enable/teardown controls.
- Implement hybrid routing for selected tables.

## Phase 7: User/Grant Migration and Reporting

- Implement account/grant migration helper with selectable scope.
- Emit detailed auth/plugin compatibility report.

## Phase 8: Hardening and Release

- Run full local matrix.
- Finalize docs, risk guidance, and release artifacts.
