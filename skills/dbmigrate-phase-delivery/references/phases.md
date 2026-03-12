# dbmigrate execution phases

Use this as the default milestone map.

## Completed (v1 core — PR #1 through #85)

### Phase 0: Research and Risk Artifacts ✅

### Phase 1: Repository Foundation ✅

### Phase 2: Connectivity and Plan Pipeline ✅

### Phase 3: Baseline Migration ✅

### Phase 4: Verification Engine ✅

### Phase 5: Incremental Replication (binlog) ✅

All v1 core is implemented and passing: 5 subcommands, 15+ prechecks, binlog replication with checkpoints, 4 data verification modes, dry-run sandbox, CI pipeline, Docker matrix with 14 services.

## Active — v1 Release Hardening

### Phase 5.1: v1 Release Gate

- Freeze v1 support matrix (strict-lts pairs).
- Run fresh full Docker matrix and archive results.
- Execute all focused rehearsals per `docs/v1-release-criteria.md`.
- Synchronize `README.md` and operator docs with frozen v1 surface.
- Complete signoff checklist and tag `v1.0.0`.

## Upcoming — v2 Features

### Phase 6: Schema Objects Migration

- Extend `internal/schema/copy.go` for stored procedures, functions, triggers, events.
- Add `SHOW CREATE PROCEDURE/FUNCTION/TRIGGER/EVENT` extraction.
- Handle DEFINER rewriting and delimiter edge cases.
- Extend `internal/verify/schema/verify.go` for routine/trigger/event comparison.
- Update `--include-objects` to accept `routines,triggers,events`.
- Add prechecks for cross-engine routine incompatibilities.
- See `skills/dbmigrate-schema-objects` for detailed guidance.

### Phase 7: Trigger-CDC and Hybrid Replication

- Design CDC log table schema and trigger generation.
- Implement `internal/replicate/cdc/` package.
- Implement `--replication-mode=capture-triggers` with explicit `--enable-trigger-cdc` / `--teardown-cdc`.
- Implement `--replication-mode=hybrid` for per-table routing.
- Implement GTID support as `--start-from=gtid`.
- See `skills/dbmigrate-replication-cdc` for detailed guidance.

### Phase 8: User/Grant Migration

- Implement extraction from `mysql.user` + `SHOW GRANTS`.
- Selectable scope: business accounts only vs. include system accounts.
- Auth plugin compatibility report with per-plugin remediation.
- Apply grants on destination with rollback safety.

### Phase 9: Concurrency and Observability

- Real concurrent data copy using goroutines + semaphore (honor `--concurrency` flag).
- Structured JSON logging with configurable levels.
- Progress reporting: throughput, rows/sec, ETA per table.
- Rate limiting for migration operations.
- See `skills/dbmigrate-code-quality` for patterns.

### Phase 10: Granular Schema Verification

- Column-by-column verification via `INFORMATION_SCHEMA.COLUMNS`.
- Index verification via `INFORMATION_SCHEMA.STATISTICS`.
- Foreign key constraint verification.
- Partition verification.
- Implement flags: `--tolerate-collation-diffs`, `--ignore-table-options`.

### Phase 11: Hardening and v2 Release

- Full Docker matrix for all v2 features.
- Update operator docs for new object types and replication modes.
- Performance regression test suite.
- Tag `v2.0.0`.
