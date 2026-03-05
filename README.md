# dbmigrate

`dbmigrate` is a Go CLI for migration and consistency verification between MySQL-family databases, with planned incremental replication support.

## Current status

This repository is in phased development.

Completed milestones (merged through Phase 23):
- Phase 0: research and operator risk docs
- Phase 1: foundation scaffold + CI baseline
- Phase 2: runtime config, DSN validation, config file support
- Phase 3: baseline schema migration (tables/views)
- Phase 4: baseline data migration with checkpoint/resume
- Phase 5-9: verification pipeline (schema + count/hash/sample/full-hash data modes)
- Phase 10-15: replication baseline, checkpoint safety, transactional apply, binlog decode/apply pipeline
- Phase 16-18: conflict policy + DDL safety + SQL error categorization
- Phase 19-21: conflict report enrichment (`value_sample`, `old_row_sample`, `new_row_sample`)
- Phase 22: selectable downgrade profiles (`strict-lts`, `same-major`, `adjacent-minor`, `max-compat`)
- Phase 23: explicit strict-lts downgrade matrix hardening

Current process:
- Delivery is phase-based via small PRs from `codex/*` branches.
- Each phase runs local full tests and required CI checks before merge.

Pending next milestones:
- Continue replication/report ergonomics hardening and operator-facing diagnostics.
- Expand strict compatibility matrices where needed with explicit version/range policies.
- Keep documentation and runbooks synchronized with merged phases.

## Supported migration priorities

1. MariaDB -> MariaDB (upgrade/downgrade)
2. MySQL -> MySQL (upgrade/downgrade)
3. MariaDB <-> MySQL cross-engine

## Commands (scaffold)

```bash
dbmigrate plan --source "mysql://user:pass@host:3306/" --dest "mysql://user:pass@host:3306/"
dbmigrate migrate --source "mysql://..." --dest "mysql://..."
dbmigrate replicate --source "mysql://..." --dest "mysql://..."
dbmigrate verify --source "mysql://..." --dest "mysql://..."
dbmigrate report --source "mysql://..." --dest "mysql://..." --json
```

Downgrade profile selection for `plan`:

```bash
dbmigrate plan --source "mysql://..." --dest "mysql://..." --downgrade-profile strict-lts
```

Supported profiles:
- `strict-lts` (default)
- `same-major`
- `adjacent-minor`
- `max-compat`

`strict-lts` explicit same-engine matrix:
- `MySQL 8.0.x -> MySQL 8.0.x`
- `MySQL 8.4.x -> MySQL 8.4.x`
- `MariaDB 10.11.x -> MariaDB 10.11.x`
- `MariaDB 11.4.x -> MariaDB 11.4.x`

## Baseline migration modes

```bash
# Schema-only baseline
dbmigrate migrate --source "mysql://..." --dest "mysql://..." --schema-only

# Data-only baseline in chunks (resume from checkpoint on retry)
dbmigrate migrate --source "mysql://..." --dest "mysql://..." --data-only --chunk-size 1000 --resume

# Full baseline (schema + data)
dbmigrate migrate --source "mysql://..." --dest "mysql://..." --chunk-size 1000
```

## Incremental replication baseline

```bash
# Replication run with preflight + checkpoint safety tracking
dbmigrate replicate --source "mysql://..." --dest "mysql://..." --resume --apply-ddl warn --conflict-policy fail

# Start from explicit binlog file/position when no checkpoint exists
dbmigrate replicate --source "mysql://..." --dest "mysql://..." --resume=false --start-file mysql-bin.000001 --start-pos 4 --conflict-policy fail
```

Replication preflight requirements:
- source `log_bin` must be enabled
- source `binlog_format` must be `ROW`
- source `binlog_row_image` must be `FULL`

Replication checkpoint safety behavior:
- The summary includes `start`, `source_end`, `applied_end`, and `applied_events`.
- Checkpoint advances only to `applied_end` (never directly to `source_end`).
- Apply path is transaction-batch based; checkpoint advances only after destination commit succeeds.
- Binlog event loading/decoding now maps row events into destination SQL batches with fail-fast behavior on unsupported patterns.
- Conflict policy is explicit via `--conflict-policy={fail,source-wins,dest-wins}` (default: `fail`).
- DDL safety in `--apply-ddl=apply` mode allows only low-risk DDL; risky DDL fails with remediation guidance.
- On replication failure, a detailed JSON report is written to `--state-dir/replication-conflict-report.json`.
- Conflict reports include `failure_type` categorization, `sql_error_code` (when available), key/value context (`value_sample`), and row-level context (`old_row_sample`, `new_row_sample`, `row_diff_sample`) for debugging.

## Report command (state artifacts)

```bash
# JSON-first detailed operator report from --state-dir artifacts
dbmigrate report --state-dir ./state --json
```

Current report behavior:
- Reads local state artifacts when present:
  - `data-baseline-checkpoint.json`
  - `replication-checkpoint.json`
  - `replication-conflict-report.json`
- Emits status:
  - `ok` when no conflict report failure is present
  - `attention_required` when a replication conflict failure is present
  - `empty` when no known state artifacts are found
- Includes remediation proposals from conflict reports in the `proposals` section.

## Verification modes

```bash
# Schema diff verification (tables/views)
dbmigrate verify --source "mysql://..." --dest "mysql://..." --verify-level schema

# Data verification by deterministic row-count comparison
dbmigrate verify --source "mysql://..." --dest "mysql://..." --verify-level data --data-mode count

# Data verification by deterministic table content hash
dbmigrate verify --source "mysql://..." --dest "mysql://..." --verify-level data --data-mode hash

# Data verification by deterministic sampled rows hash
dbmigrate verify --source "mysql://..." --dest "mysql://..." --verify-level data --data-mode sample --sample-size 1000

# Data verification by deterministic full-table hash mode
dbmigrate verify --source "mysql://..." --dest "mysql://..." --verify-level data --data-mode full-hash
```

## Configuration file support (phase 2)

Use `--config <path>` to load YAML/JSON runtime options.
When both are present, explicit CLI flags override config-file values.
You can set `downgrade-profile` (YAML) or `downgrade_profile` (JSON) in config files.

## Build

```bash
go build -trimpath -ldflags="-s -w" -o bin/dbmigrate ./cmd/dbmigrate
```

## Local checks

```bash
make fmt
make test
```

If `golangci-lint` and `govulncheck` are installed:

```bash
make lint
make vulncheck
```

## Safety notes

- Incompatible features are designed to fail fast.
- Downgrade incompatibilities fail with non-zero exit code and include remediation proposals in plan/report output.
- DDL application policy is controlled only by `--apply-ddl={ignore,apply,warn}`.
- Detailed migration risks and mitigations are documented in [docs/known-problems.md](docs/known-problems.md).

## Documentation

- [Development plan](docs/development-plan.md)
- [Known migration problems](docs/known-problems.md)
- [Operator risk checklist](docs/risk-checklist.md)
- [Operators guide](docs/operators-guide.md)
- [Security notes](docs/security.md)
- [Project implementation instructions](Instructions.md)
