---
name: dbmigrate-replication-cdc
description: Implement trigger-based CDC, hybrid replication mode, and GTID support. Use when building the v2 replication features that extend beyond the v1 binlog-only mode, including fallback CDC for environments without binlog access and per-table routing.
---

# dbmigrate replication CDC and hybrid

## Objective

Implement the three remaining replication modes reserved for v2:
1. `--replication-mode=capture-triggers` — trigger-based CDC fallback.
2. `--replication-mode=hybrid` — per-table routing between binlog and trigger-CDC.
3. `--start-from=gtid` — GTID-based start position for binlog replication.

## Architecture context

Current binlog replication lives in `internal/replicate/binlog/`:

- `run.go` — `Run()` orchestrates checkpoint loading, preflight, batch loading, transactional apply, and checkpoint saving.
- `load.go` — `streamWindowEvents()` connects via `go-mysql-org/go-mysql` BinlogSyncer, streams events, converts to `streamEvent`, builds `applyBatch` slices.
- `failure.go` — `applyFailure` struct with type, remediation, context for structured error reporting.
- `shape.go` — transaction shape tracking for diagnostics.

Key patterns to follow:
- Dependency injection via package-level function vars for testability.
- `applyBatch` / `applyEvent` model for transactional apply.
- `applyFailure` for structured error reporting with remediation.
- Checkpoint persistence via `internal/state/`.
- Conflict policy handling (fail/source-wins/dest-wins).

The CLI already reserves flags in `internal/commands/replicate.go` that fail-fast with an error message. These need to be wired to real implementations.

## Implementation plan

### PR 1: CDC log table design and trigger generation

1. Create `internal/replicate/cdc/` package.

2. Design CDC log table schema:
   ```sql
   CREATE TABLE <schema>.__dbmigrate_cdc_log (
     cdc_id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
     table_name VARCHAR(255) NOT NULL,
     operation ENUM('INSERT','UPDATE','DELETE') NOT NULL,
     old_row_json JSON,
     new_row_json JSON,
     captured_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
   ) ENGINE=InnoDB
   ```

3. Implement trigger generation functions:
   - `generateInsertTrigger(schema, table, columns)` — captures NEW row as JSON.
   - `generateUpdateTrigger(schema, table, columns)` — captures OLD and NEW rows.
   - `generateDeleteTrigger(schema, table, columns)` — captures OLD row.

4. Implement `SetupCDC(ctx, source, schema, tables)` — creates log table + triggers.

5. Implement `TeardownCDC(ctx, source, schema, tables)` — drops triggers + log table.

### PR 2: CDC event reader and batch builder

1. Implement `ReadCDCEvents(ctx, source, schema, fromID, limit)`:
   - Query CDC log table ordered by `cdc_id`.
   - Convert JSON rows back to `[]any` column values.
   - Map to the same `applyBatch`/`applyEvent` model used by binlog.

2. Implement checkpoint integration:
   - Store last processed `cdc_id` in replication checkpoint.
   - Resume from last `cdc_id` on restart.

3. Implement purge of processed CDC log entries.

### PR 3: CDC apply pipeline

1. Implement `cdc.Run(ctx, source, dest, stateDir, opts)`:
   - Mirror the structure of `binlog.Run()`.
   - Preflight: verify CDC triggers exist on source.
   - Read batches from CDC log.
   - Apply transactionally with same conflict policy handling.
   - Save checkpoint.

2. Wire `--replication-mode=capture-triggers` in `internal/commands/replicate.go`.

3. Wire `--enable-trigger-cdc` and `--teardown-cdc` flags.

### PR 4: Hybrid replication mode

1. Implement `hybrid.Run(ctx, source, dest, stateDir, opts)`:
   - Accept per-table routing config: which tables use binlog vs trigger-CDC.
   - Coordinate both replication streams.
   - Merge checkpoint state.

2. Wire `--replication-mode=hybrid` in replicate command.

3. Add routing configuration (either via flag or config file).

### PR 5: GTID support

1. Extend `binlog.Run()` to accept GTID-based start:
   - Parse `--start-from=gtid:<gtid_set>`.
   - Use `syncer.StartSyncGTID()` instead of `syncer.StartSync()` when GTID provided.

2. Handle MySQL GTID vs MariaDB GTID:
   - MySQL: `source_uuid:transaction_id` format.
   - MariaDB: `domain_id-server_id-sequence` format.
   - Auto-detect from source engine type.

3. Store GTID set in checkpoint for resume.

4. Add precheck: verify `@@gtid_mode` on source (MySQL) or GTID availability (MariaDB).

## Testing strategy

- Unit tests with injected function vars (no real DB).
- Integration tests:
  - CDC setup/apply/teardown lifecycle.
  - CDC with concurrent source writes.
  - Hybrid mode with mixed table routing.
  - GTID start and resume.
  - Cross-engine CDC (MariaDB source triggers → MySQL dest apply).

## Key risks

- **Trigger overhead on source:** CDC triggers add write amplification. Document performance impact.
- **JSON column representation:** `JSON_OBJECT()` output varies between MySQL and MariaDB. Use explicit column serialization.
- **Concurrent CDC reads:** multiple replicate runs could read same CDC log. Use `cdc_id` watermark with `FOR UPDATE SKIP LOCKED` or similar.
- **GTID incompatibility:** MySQL and MariaDB GTID formats are completely incompatible. Cross-engine GTID start is not supported.
- **Trigger naming conflicts:** generated trigger names must be deterministic and avoid collisions with user triggers. Use `__dbmigrate_` prefix.
