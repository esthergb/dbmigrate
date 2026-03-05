# Operators Guide (Draft)

## Intended workflow

1. Run `dbmigrate plan` and review compatibility warnings.
2. Run baseline migration (`dbmigrate migrate`).
3. Run periodic incremental sync (`dbmigrate replicate`).
4. Run verification (`dbmigrate verify`).
5. Generate machine-readable report (`dbmigrate report --json`).

Compatibility profile selection:
- `dbmigrate plan --source "<dsn>" --dest "<dsn>" --downgrade-profile strict-lts`
- Supported values: `strict-lts` (default), `same-major`, `adjacent-minor`, `max-compat`.
- `strict-lts` explicit same-engine matrix:
  - `MySQL 8.0.x -> MySQL 8.0.x`
  - `MySQL 8.4.x -> MySQL 8.4.x`
  - `MariaDB 10.11.x -> MariaDB 10.11.x`
  - `MariaDB 11.4.x -> MariaDB 11.4.x`

## Baseline migration execution

- Schema-only:
  - `dbmigrate migrate --source "<dsn>" --dest "<dsn>" --schema-only`
- Data-only:
  - `dbmigrate migrate --source "<dsn>" --dest "<dsn>" --data-only --chunk-size 1000`
- Full baseline:
  - `dbmigrate migrate --source "<dsn>" --dest "<dsn>" --chunk-size 1000`

Checkpoint and resume:
- Baseline data copy writes checkpoint state into `--state-dir` (default `./state`).
- Use `--resume` to continue from checkpoint state after interruption.
- Current resume strategy restarts incomplete tables safely (truncate/delete fallback) to avoid duplication.

## Verification execution

- Schema verification (tables/views):
  - `dbmigrate verify --source "<dsn>" --dest "<dsn>" --verify-level schema`
- Data verification (current baseline mode):
  - `dbmigrate verify --source "<dsn>" --dest "<dsn>" --verify-level data --data-mode count`
- Data verification (table hash mode):
  - `dbmigrate verify --source "<dsn>" --dest "<dsn>" --verify-level data --data-mode hash`
- Data verification (sample hash mode):
  - `dbmigrate verify --source "<dsn>" --dest "<dsn>" --verify-level data --data-mode sample --sample-size 1000`
- Data verification (full hash mode):
  - `dbmigrate verify --source "<dsn>" --dest "<dsn>" --verify-level data --data-mode full-hash`

Verification behavior:
- Any diff returns non-zero exit code.
- `--json` emits structured diff details for automation pipelines.
- `sample` mode uses `--sample-size` rows per table; `full-hash` hashes full table content.

## Incremental replication checkpoint execution

- Resume from saved checkpoint:
  - `dbmigrate replicate --source "<dsn>" --dest "<dsn>" --resume --apply-ddl warn --conflict-policy fail`
- Start from explicit source position:
  - `dbmigrate replicate --source "<dsn>" --dest "<dsn>" --resume=false --start-file mysql-bin.000001 --start-pos 4 --apply-ddl warn --conflict-policy fail`

Replication checkpoint behavior:
- Checkpoint file path: `--state-dir/replication-checkpoint.json`.
- Conflict report path on failure: `--state-dir/replication-conflict-report.json`.
- Supported DDL policy values are restricted to `--apply-ddl={ignore,apply,warn}`.
- Supported conflict policies are `--conflict-policy={fail,source-wins,dest-wins}`.
- Run summary reports `start`, `source_end`, `applied_end`, and `applied_events`.
- Checkpoint advancement is tied to `applied_end` only (never directly to source tip).
- Event application is transaction-batch based; checkpoint advances only after commit.
- Row-based binlog events are decoded into SQL apply batches (insert upsert, update, delete) with commit-boundary checkpointing.
- `--apply-ddl=apply` is safety-classified: risky DDL (drop/rename/destructive alter patterns) is blocked with remediation guidance.
- Source preflight gates: `log_bin=ON`, `binlog_format=ROW`, and `binlog_row_image=FULL`.
- Conflict report JSON includes categorized `failure_type` values (for example `schema_drift`, `conflict_duplicate_key`), `sql_error_code` when surfaced by the destination engine, and contextual samples: `value_sample`, `old_row_sample`, `new_row_sample`, `row_diff_sample`.

## Safety defaults

- Fail fast on known incompatible features.
- Downgrade incompatibilities must fail with detailed remediation proposals.
- Use conservative conflict policy (`fail`).
- Use explicit DDL policy via `--apply-ddl={ignore,apply,warn}`.

## Rollback strategy (to refine)

- Take source and destination backups before baseline migration.
- Keep replication checkpoints immutable per successful run.
- If verification fails, stop apply loop and inspect report before retry.
