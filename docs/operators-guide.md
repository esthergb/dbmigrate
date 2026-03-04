# Operators Guide (Draft)

## Intended workflow

1. Run `dbmigrate plan` and review compatibility warnings.
2. Run baseline migration (`dbmigrate migrate`).
3. Run periodic incremental sync (`dbmigrate replicate`).
4. Run verification (`dbmigrate verify`).
5. Generate machine-readable report (`dbmigrate report --json`).

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

## Safety defaults

- Fail fast on known incompatible features.
- Use conservative conflict policy (`fail`).
- Use explicit DDL policy via `--apply-ddl={ignore,apply,warn}`.

## Rollback strategy (to refine)

- Take source and destination backups before baseline migration.
- Keep replication checkpoints immutable per successful run.
- If verification fails, stop apply loop and inspect report before retry.
