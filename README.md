# dbmigrate

`dbmigrate` is a Go CLI for migration, incremental replication, and consistency verification between MySQL-family databases.

Operational guardrail:
- Use global `--operation-timeout=<duration>` to bound end-to-end command runtime. `0` disables the deadline.

## Current status

This repository is in phased development, with merged work through Phase 64.

Merged capability families:
- research and operator risk baseline
- foundation scaffold, CI, runtime config, and DSN/config-file handling
- baseline schema migration and chunked data migration with resume/checkpoints
- schema/data verification (`count`, `hash`, `sample`, `full-hash`)
- incremental replication in implemented binlog mode
- conflict policy, DDL safety, and conflict-report enrichment
- downgrade-profile enforcement and compatibility prechecks
- operator rehearsals for metadata locks, rollback evidence, time-zone drift, plugin lifecycle, transaction shape, hidden schema, collation risk, and verify canonicalization

Current process:
- Delivery is phase-based via small PRs from `codex/*` branches.
- Each phase runs local full tests and required CI checks before merge.
- Matrix test scripts include compatibility probes that exercise version/engine-specific SQL behavior differences.

Current release stance:
- `v1`: only implemented and genuinely supported self-managed paths
- `v2`: currently surfaced but not yet implemented CLI paths
- `v3`: managed/cloud deployment support

## Supported in v1

- self-managed deployments only
- baseline migrate workflow:
  - `plan`
  - `migrate`
  - `verify`
  - `report`
- replication in implemented binlog mode only
- current merged fail-fast prechecks and report semantics already on `main`

Release-grade v1 support is limited to exact engine/version pairs validated by the frozen support matrix and fresh release evidence.

## Not supported in v1

- managed/cloud deployment support
- any engine/version pair not included in the frozen v1 matrix
- any path blocked by current prechecks as incompatible by design
- any cross-engine candidate pair not yet promoted into the frozen v1 matrix

## Reserved for v2

- `--replication-mode=capture-triggers`
- `--replication-mode=hybrid`
- `--enable-trigger-cdc`
- `--teardown-cdc`
- `--start-from=gtid`

These names are intentionally reserved in the CLI surface, but they are not part of `v1`.

Object scope in `v1`:
- Default `--include-objects` is `tables,views`.
- Requesting `routines`, `triggers`, or `events` in `v1` fails fast with incompatibility exit semantics (`exit 2`) and explicit "reserved for v2" guidance.
- Default `--tls-mode` is `required`.
- `--tls-mode=preferred` is explicit opt-in and emits a plaintext-fallback warning at runtime.

## Reserved for v3

- managed/cloud qualification for platforms such as RDS, Cloud SQL, Azure Database for MySQL, and Aurora-like offerings
- managed failover and provider-specific durability guarantees as a release-grade support promise

## Latest validation snapshot (2026-03-05)

Detailed local execution evidence (Apple Silicon, `docker compose`):

- Full migration matrix (20 scenarios): `reports/matrix-phase56/20260305T224136Z/summary.tsv`
  - Result: `20/20` scenarios successful (all exit code `0`).
- Compatibility probe sweep (5 services): `state/probe-validation/*.json`
  - Probe pack size: `20` probes per service.
  - Service results:
    - `mariadb10` (`10.6.25`): `ok=9`, `failed=11`
    - `mariadb11` (`11.0.6`): `ok=9`, `failed=11`
    - `mariadb12` (`12.0.2`): `ok=10`, `failed=10`
    - `mysql80` (`8.0.45`): `ok=10`, `failed=10`
    - `mysql84` (`8.4.8`): `ok=8`, `failed=12`
- Added and validated `1067 Invalid default value` simulation probes:
  - `zero_datetime_default_strict`
  - `zero_timestamp_default_strict`
  - `zero_date_default_strict`
  - `zero_in_date_default_strict`
  - `invalid_calendar_date_default`
  - `invalid_calendar_datetime_default`
  - `timestamp_out_of_range_default`
- Observed behavior:
  - MariaDB defaults (`10.6/11.0/12.0`) accept zero-date defaults in current `sql_mode`.
  - MySQL defaults (`8.0/8.4`) reject zero-date/zero-in-date defaults in strict mode (`NO_ZERO_DATE`, `NO_ZERO_IN_DATE`).
  - Invalid calendar defaults and out-of-range timestamp defaults fail across both engines (same `1067` family).

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
- `MySQL 8.4.x -> MySQL 8.4.x`
- `MariaDB 10.11.x -> MariaDB 10.11.x`
- `MariaDB 11.4.x -> MariaDB 11.4.x`
- `MariaDB 11.8.x -> MariaDB 11.8.x`

`same-major` explicit same-engine matrix ranges:
- `MySQL 8.4-8.4 -> MySQL 8.4-8.4` (same major required)
- `MariaDB 10.11-10.11 -> MariaDB 10.11-10.11` (same major required)
- `MariaDB 11.4-11.8 -> MariaDB 11.4-11.8` (same major required)

`adjacent-minor` explicit same-engine matrix ranges:
- `MySQL 8.4-8.4 -> MySQL 8.4-8.4` (same major + one minor step max)
- `MariaDB 10.11-10.11 -> MariaDB 10.11-10.11` (same major + one minor step max)
- `MariaDB 11.4-11.8 -> MariaDB 11.4-11.8` (same major + one minor step max)

`strict-lts` explicit cross-engine matrix pairs:
- `MySQL 8.4.x -> MariaDB 11.4.x`
- `MariaDB 11.4.x -> MySQL 8.4.x`

Profile scope note:
- `same-major` and `adjacent-minor` are same-engine only (cross-engine paths are blocked by default).
- Use `strict-lts` for release-grade matrix-based validation.
- `max-compat` remains available for permissive risk-reviewed paths, but it is not release-grade `v1` support unless a path is explicitly promoted into the frozen matrix.
- Under `max-compat`, legacy lines (for example MySQL 8.0.x and MariaDB 10.6.x) are allowed but explicitly warned in findings.
- Under `max-compat`, `MySQL 8.4.x <-> MariaDB 11.8.x` is flagged as an active-LTS candidate pair pending strict-lts validation.
- `plan` output surfaces `report.requires_evidence=true` for active-LTS candidate paths that still need repeated staged validation evidence before strict-lts promotion.

## Baseline migration modes

```bash
# Schema-only baseline
dbmigrate migrate --source "mysql://..." --dest "mysql://..." --schema-only

# Data-only baseline in chunks (resume from checkpoint on retry)
dbmigrate migrate --source "mysql://..." --dest "mysql://..." --data-only --chunk-size 1000 --resume

# Full baseline (schema + data)
dbmigrate migrate --source "mysql://..." --dest "mysql://..." --chunk-size 1000
```

Schema apply behavior:
- View DDL `DEFINER=` clauses from source are sanitized to `DEFINER=CURRENT_USER` during apply to avoid orphan/unknown definer failures on destination.
- Intra-database foreign-key cycles are not auto-rewritten in `v1`; schema/data baseline fails fast and requires a manual post-step for cyclic constraints.

State artifact behavior:
- `dbmigrate` enforces a single-writer lock per `--state-dir`; concurrent `plan`/`migrate`/`replicate`/`verify` runs against the same directory fail fast instead of racing checkpoint/report artifacts.
- If a process crashes and leaves `.dbmigrate.lock` behind, dbmigrate fails with the lock path plus owner metadata. v1 recovery is manual by design: verify no live dbmigrate process still owns that state-dir, remove the stale lock file, then rerun.

## Schema precheck: zero-date defaults

- `plan` and `migrate` now run a schema precheck that scans source temporal defaults (`DATE`, `DATETIME`, `TIMESTAMP`) for zero-date patterns (`0000-00-00`, `0000-00-00 00:00:00`, `YYYY-00-DD`, `YYYY-MM-00`).
- When destination `sql_mode` enforces strict zero-date validation (`STRICT_*` + `NO_ZERO_DATE`/`NO_ZERO_IN_DATE`), the command fails fast with:
  - detailed findings per affected column
  - auto-fix SQL proposals (`ALTER TABLE ... ALTER COLUMN ... SET DEFAULT ...`)
- When incompatibilities are found, dbmigrate also writes an auto-fix artifact at:
  - `--state-dir/precheck-zero-date-fixes.sql`
- This precheck returns incompatibility exit semantics (`exit 2`) rather than runtime crash semantics.

## Schema precheck: foreign-key cycles

- `plan` now detects intra-database foreign-key cycles in selected scope and fails compatibility when found.
- `v1` does not auto-rewrite cyclic constraints.
- Recommended remediation:
  - create/load the affected tables without the cyclic foreign keys
  - add the cyclic constraints in a controlled manual post-step with `ALTER TABLE`

## Schema precheck: JSON cross-engine and MariaDB-only features

- `plan` now inventories additional documented schema-feature risks:
  - JSON columns on cross-engine paths
  - MariaDB `SEQUENCE` objects
  - MariaDB system-versioned tables (`WITH SYSTEM VERSIONING`)
- v1 behavior:
  - cross-engine JSON columns are treated as incompatible
  - MariaDB sequences are incompatible on non-MariaDB destinations
  - MariaDB system-versioned tables are incompatible on non-MariaDB destinations
  - system-versioned tables still emit warnings inside MariaDB-only lanes because replication/binlog semantics need careful rehearsal

## Schema precheck: identifier portability and parser drift

- `plan` now inventories identifier-portability risks that often surface only after version or engine changes:
  - identifiers that collide with destination reserved words
  - mixed-case database/table/view names across `lower_case_table_names` boundaries
  - case-fold collisions such as `Orders` and `orders`
  - view definitions that depend on SQL-mode parser behavior (`ANSI_QUOTES`, `PIPES_AS_CONCAT`, `NO_BACKSLASH_ESCAPES`)
- `migrate` fails on the same incompatibilities before schema apply.
- Current v1 policy:
  - identifiers that become newly reserved on the destination are incompatible
  - identifiers already reserved on both source and destination stay warning-level, because legal quoted schemas can still exist
  - `lower_case_table_names` mismatches plus case-fold collisions are incompatible
  - mixed-case names become incompatible when either side uses case-folding semantics
  - parser-sensitive view definitions fail until the view is rewritten or SQL-mode semantics are aligned

## Replication precheck: cross-engine GTID boundary inventory

- `plan` now inventories cross-engine continuity boundary evidence for replication lanes:
  - source/destination GTID state
  - source `log_bin`
  - source `binlog_format`
  - MySQL -> MariaDB row-event settings such as `binlog_row_value_options` and `binlog_transaction_compression`
- Current v1 policy:
  - cross-engine GTID state is reported as a warning class, not as the resume contract
  - v1 cross-engine continuity must still use file/position, not GTID auto-position
  - boundary warnings remain visible in `plan` even when baseline migration itself is otherwise compatible

## Replication precheck: source binlog readiness

- `plan` now inventories source replication-readiness settings used by `replicate`:
  - `log_bin`
  - `binlog_format`
  - `binlog_row_image`
  - current binary log handoff position when visible
- Current v1 policy:
  - these findings are warning-level in `plan`
  - `replicate` still enforces them as hard runtime gates
  - use `plan` to detect the misconfiguration earlier, not to downgrade the runtime safety fence

## Temporal precheck: time-zone portability

- `plan` now inventories:
  - source/destination `system_time_zone`
  - source/destination global/session `time_zone`
  - tables containing `TIMESTAMP` and `DATETIME`
  - tables that mix both types
- Current v1 policy:
  - time-zone drift and mixed `TIMESTAMP`/`DATETIME` semantics are warning-level portability findings
  - they do not block baseline migration automatically
  - they must be reviewed before claiming application-level temporal compatibility

## Data precheck: stable keys and representation-sensitive tables

- `plan` now inventories:
  - tables without a primary key or non-null unique key
  - representation-sensitive tables containing approximate numerics, temporal columns, JSON, or collation-sensitive text
- Current v1 policy:
  - keyless tables are incompatible because live baseline migration and deterministic verify modes depend on stable keys
  - representation-sensitive tables stay warning-level and require canonicalized verify evidence

## Manual evidence findings

- `plan` now also emits warning-level findings for documented classes that cannot be proven from metadata alone:
  - backup/restore usability evidence
  - metadata-lock runbook readiness
  - transaction-shape rehearsal
  - dump/import tool skew review
  - view definer rewrite review when views are in scope
  - source grant inventory hints for replication workflows

## Incremental replication baseline

```bash
# Replication run with preflight + checkpoint safety tracking
dbmigrate replicate --source "mysql://..." --dest "mysql://..." --replication-mode binlog --start-from auto --resume --apply-ddl warn --conflict-policy fail

# Start from explicit binlog file/position when no checkpoint exists
dbmigrate replicate --source "mysql://..." --dest "mysql://..." --replication-mode binlog --start-from binlog-file:pos --resume=false --start-file mysql-bin.000001 --start-pos 4 --conflict-policy fail
```

Replication mode selection:
- `--replication-mode=binlog` is the currently implemented mode.
- `--replication-mode=capture-triggers` and `--replication-mode=hybrid` are reserved for `v2`; today they fail fast with an explicit "not implemented yet" error.
- `--enable-trigger-cdc` and `--teardown-cdc` are also reserved for `v2` and currently fail fast with explicit guidance.

Replication start selection:
- `--start-from=auto` (default) uses checkpoint/resume behavior.
- `--start-from=binlog-file:pos` requires `--resume=false` plus explicit `--start-file` and `--start-pos`.
- `--start-file` must be a bare binlog filename; path-like values and invalid characters are rejected before connect/apply.
- `--start-from=gtid` is reserved for `v2` and currently fails fast with explicit guidance.

Replication window control:
- `--max-events=0` (default) applies all available events in the selected window.
- `--max-events=N` limits apply work by transaction boundaries (never checkpoints partial transactions).
- If the first transaction already exceeds `N`, replicate fails fast with guidance to increase the limit.
- `--max-lag-seconds=0` (default) disables lag threshold checks.
- `--max-lag-seconds=N` blocks apply when the transaction-end event lag exceeds `N` seconds (based on binlog event timestamps).
- `--source-server-id=0` (default) derives a replication client `server_id` from source DSN fields.
- `--source-server-id=N` overrides the derived value (`1..4294967295`) to avoid collisions across concurrent replication workers.

Idempotent replay guard:
- `--idempotent` is reserved for `v2` and is currently unsupported in `v1`.
- Using `--idempotent` fails fast with incompatibility exit semantics (`exit 2`).

Replication preflight requirements:
- source `log_bin` must be enabled
- source `binlog_format` must be `ROW`
- source `binlog_row_image` must be `FULL`

Replication checkpoint safety behavior:
- The summary includes `start`, `source_end`, `applied_end`, and `applied_events`.
- Checkpoint advances only to `applied_end` (never directly to `source_end`).
- Apply path is transaction-batch based; checkpoint advances only after destination commit succeeds.
- Destination checkpoint state is also persisted in `dbmigrate_replication_checkpoint` and written atomically in the same destination transaction as applied row changes.
- Binlog event loading/decoding now maps row events into destination SQL batches with fail-fast behavior on unsupported patterns.
- Replication fails fast when a replay window mixes DDL and row events; v1 requires schema-aligned windows because row-event mapping currently relies on live metadata.
- Replication enforces bounded source-window buffering (event count + estimated bytes) while reading binlog events; oversized windows fail fast as `failure_type=source_window_buffer_limit_exceeded` with remediation to shorten replay windows.
- Keyless `UPDATE`/`DELETE` replay is blocked as unsafe and fails fast with remediation guidance.
- Conflict policy is explicit via `--conflict-policy={fail,source-wins,dest-wins}` (default: `fail`).
- DDL safety in `--apply-ddl=apply` mode allows only low-risk DDL; risky DDL fails with remediation guidance.
- On replication failure, a detailed JSON report is written to `--state-dir/replication-conflict-report.json`.
- On replication success, stale `--state-dir/replication-conflict-report.json` from previous failed runs is removed.
- Conflict reports include `failure_type` categorization, `sql_error_code` (when available), key/value context (`value_sample`), and row-level context (`old_row_sample`, `new_row_sample`, `row_diff_sample`) for debugging.
- Conflict artifact samples are `redacted` by default. Use `--conflict-values=plain` only as explicit opt-in in controlled environments.

## Report command (state artifacts)

```bash
# JSON-first detailed operator report from --state-dir artifacts
dbmigrate report --state-dir ./state --json

# Override fail-fast behavior to emit report without non-zero exit
dbmigrate report --state-dir ./state --json --fail-on-conflict=false
```

Current report behavior:
- Reads local state artifacts when present:
  - `collation-precheck.json`
  - `verify-data-report.json`
  - `data-baseline-checkpoint.json`
  - `replication-checkpoint.json`
  - `replication-conflict-report.json`
- Emits status:
  - `ok` when no incompatible precheck artifact or active conflict artifact is present
  - `attention_required` when an incompatible precheck artifact or active replication conflict artifact is present
  - `empty` when no known state artifacts are found
- Verify-data artifacts can keep `status=ok` when verification passed but representation-sensitive tables still deserve evidence retention.
- Stale conflict reports are auto-ignored when replication checkpoint position has advanced beyond report `applied_end_*`, or (for legacy artifacts) when checkpoint `updated_at` is newer than conflict `generated_at`.
- Includes remediation proposals from conflict reports in the `proposals` section.
- Includes remediation proposals from incompatible precheck and verify-data artifacts when present.
- Report output distinguishes security handling for conflict samples:
  - redacted by default
  - plain-text only when explicitly requested with `--include-sensitive-artifacts`
- Fails by default (`exit 2`) when report status is `attention_required`. Use `--fail-on-conflict=false` to emit report without failing.

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

# Data verification by deterministic full-table streaming hash mode
dbmigrate verify --source "mysql://..." --dest "mysql://..." --verify-level data --data-mode full-hash
```

Verify data-mode semantics in `v1`:
- `sample`: bounded sample only (`--sample-size`), intended for fast triage.
- `hash`: full-table deterministic hash with bounded memory (chunked/streaming).
- `full-hash`: full-table deterministic hash with stricter chunked streaming aggregation (not an alias of `hash`).
- `sample`, `hash`, and `full-hash` fail fast when a table has no primary key or non-null unique key (stable order required for deterministic replay-safe hashing).

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
make release-gate-minimal
```

If `golangci-lint` and `govulncheck` are installed:

```bash
make lint
make vulncheck
make release-gate-full
```

## CI operations note

- Automatic GitHub Actions checks are the default validation path.
- Manual dispatch helper remains available for incidents where auto triggers are unavailable:

```bash
make ci-manual
```

- Optional explicit branch:

```bash
make ci-manual BRANCH=codex/feat/report-fail-default-phase27
```

- Keep this helper documented for contingency use; review periodically.

## Safety notes

- Incompatible features are designed to fail fast.
- Downgrade incompatibilities fail with non-zero exit code and include remediation proposals in plan/report output.
- Zero-date temporal defaults incompatible with destination strict `sql_mode` are blocked in `plan` and `migrate` with per-column auto-fix proposals.
- DDL application policy is controlled only by `--apply-ddl={ignore,apply,warn}`.
- Detailed migration risks and mitigations are documented in [docs/known-problems.md](docs/known-problems.md).

## Exit codes

- `0`: success
- `1`: usage/configuration error (invalid flags or global config)
- `2`: command completed but detected incompatibilities/differences (`plan` incompatible, `verify` diffs, `report` attention_required with fail-fast enabled, or `replicate` blocked by unsupported feature-gated modes/options)
- `3`: command runtime failure (`migrate`/`replicate`/other command execution error)
- `4`: `verify` runtime/tooling failure (verification could not be completed)

## Documentation

- [v1 release plan](docs/v1-release-plan.md)
- [Development plan](docs/development-plan.md)
- [Known migration problems](docs/known-problems.md)
- [Operator risk checklist](docs/risk-checklist.md)
- [Operators guide](docs/operators-guide.md)
- [Version compatibility research](docs/version-compatibility-research.md)
- [Security notes](docs/security.md)
- [Project implementation instructions](Instructions.md)
