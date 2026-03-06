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
  - `MySQL 8.4.x -> MySQL 8.4.x`
  - `MariaDB 10.11.x -> MariaDB 10.11.x`
  - `MariaDB 11.4.x -> MariaDB 11.4.x`
  - `MariaDB 11.8.x -> MariaDB 11.8.x`
- `same-major` explicit same-engine matrix ranges:
  - `MySQL 8.4-8.4 -> MySQL 8.4-8.4`
  - `MariaDB 10.11-10.11 -> MariaDB 10.11-10.11`
  - `MariaDB 11.4-11.8 -> MariaDB 11.4-11.8`
- `adjacent-minor` explicit same-engine matrix ranges (same major + one minor step max):
  - `MySQL 8.4-8.4 -> MySQL 8.4-8.4`
  - `MariaDB 10.11-10.11 -> MariaDB 10.11-10.11`
  - `MariaDB 11.4-11.8 -> MariaDB 11.4-11.8`
- `strict-lts` explicit cross-engine matrix pairs:
  - `MySQL 8.4.x -> MariaDB 11.4.x`
  - `MariaDB 11.4.x -> MySQL 8.4.x`
- Profile scope note:
  - `same-major` and `adjacent-minor` are same-engine only.
  - Use `strict-lts` for explicit cross-engine matrix validation, or `max-compat` for permissive paths with full verification.
  - `max-compat` emits explicit warnings when source/destination uses legacy lines (for example MySQL 8.0.x or MariaDB 10.6.x).
  - `max-compat` also flags `MySQL 8.4.x <-> MariaDB 11.8.x` as an active-LTS candidate pair pending strict-lts validation.
  - Active-LTS candidate paths surface `report.requires_evidence=true`; treat it as a promotion gate that requires repeated staged-run evidence before requesting strict-lts matrix inclusion.

Zero-date default precheck:
- `plan` and `migrate` execute a temporal-default precheck before schema apply.
- If destination `sql_mode` enforces strict zero-date rules (`STRICT_*` + `NO_ZERO_DATE`/`NO_ZERO_IN_DATE`), zero-date defaults in source schema fail fast.
- Findings include per-column auto-fix SQL proposals:
  - `ALTER TABLE <db>.<table> ALTER COLUMN <column> SET DEFAULT '<safe-value>';`
- A reusable fix script is generated at:
  - `--state-dir/precheck-zero-date-fixes.sql`

Plugin and engine lifecycle precheck:
- `plan` inventories source account plugins, destination active plugins, selected source table engines, and destination engine support.
- Schema `migrate` runs the same precheck before DDL apply.
- Unsupported storage engines fail fast before schema apply.
- Unsupported auth plugins are currently reported as warnings, not schema blockers, because baseline schema/data migration does not recreate accounts yet.
- Account findings label visible users as `user-managed`, `administrative`, or `system` so operators can separate application cutover work from bootstrap noise.
- MySQL `8.4` and newer may report `default_authentication_plugin` as unavailable; treat that as a signal to rely on plugin inventory, not stale variable assumptions.

Invisible-column and GIPK precheck:
- `plan` inventories source invisible columns, invisible indexes, and generated invisible primary key tables.
- Schema `migrate` runs the same precheck before DDL apply.
- MySQL -> MariaDB paths with these hidden-schema features fail fast because local rehearsal evidence shows semantic drift: hidden columns and indexes become visible, and included GIPKs stop being hidden.
- Compatible MySQL downgrade paths are still reported explicitly so operators keep dump mode intentional:
  - default dump preserves GIPK on MySQL targets that support it
  - `--skip-generated-invisible-primary-key` removes the hidden key from logical dumps entirely

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
- Replication checkpoint artifacts now also record transaction-shape signals:
  - transactions seen versus applied
  - max transaction size in apply events
  - DDL, FK, and keyless-row matching pressure
  - derived `risk_level` and `risk_signals`
- Checkpoint advancement is tied to `applied_end` only (never directly to source tip).
- Event application is transaction-batch based; checkpoint advances only after commit.
- Row-based binlog events are decoded into SQL apply batches (insert upsert, update, delete) with commit-boundary checkpointing.
- `--apply-ddl=apply` is safety-classified: risky DDL (drop/rename/destructive alter patterns) is blocked with remediation guidance.
- Source preflight gates: `log_bin=ON`, `binlog_format=ROW`, and `binlog_row_image=FULL`.
- Conflict report JSON includes categorized `failure_type` values (for example `schema_drift`, `conflict_duplicate_key`), `sql_error_code` when surfaced by the destination engine, and contextual samples: `value_sample`, `old_row_sample`, `new_row_sample`, `row_diff_sample`.

## Report generation

- Generate machine-readable report from state artifacts:
  - `dbmigrate report --state-dir ./state --json`
- Default behavior is fail-fast when conflicts are present (`status=attention_required`), returning non-zero exit.
- Optional override to keep reporting but not fail the command:
  - `dbmigrate report --state-dir ./state --json --fail-on-conflict=false`
- Report scans these files when present:
  - `data-baseline-checkpoint.json`
  - `replication-checkpoint.json`
  - `replication-conflict-report.json`
- Report status values:
  - `ok`: no conflict failure reported.
  - `attention_required`: replication conflict report contains a failure.
  - `empty`: no known artifacts found in `--state-dir`.
- `proposals` includes remediation guidance from the conflict report to help triage and rerun planning.

Metadata-lock classification:
- Replication DDL failures that time out while waiting on metadata locks are reported as `failure_type=metadata_lock_timeout` instead of a generic retryable lock error.
- The remediation text points to `SHOW FULL PROCESSLIST` plus metadata-lock instrumentation so the operator can identify the blocker before blindly retrying.

## Safety defaults

- Fail fast on known incompatible features.
- Downgrade incompatibilities must fail with detailed remediation proposals.
- Zero-date temporal defaults incompatible with destination strict `sql_mode` fail precheck with explicit auto-fix proposals.
- Unsupported source storage engines fail precheck before schema apply.
- Unsupported auth plugins are surfaced explicitly in `plan` findings so account cutover work is not guessed later.
- Invisible columns, invisible indexes, and GIPK drift fail precheck when the destination cannot preserve hidden-schema semantics.
- Use conservative conflict policy (`fail`).
- Use explicit DDL policy via `--apply-ddl={ignore,apply,warn}`.
- Treat replication worker count as secondary to transaction shape; one huge commit still behaves like one serialization unit.

## Metadata-lock incident triage

Why this matters:
- A waiting DDL can block later ordinary reads and writes behind it, making the system look generally unhealthy even when the real blocker is one older transaction.
- The dangerous move is retrying or stacking more DDL before identifying the blocker.

Recommended rehearsal:
- Start one service with `docker compose up -d <service>`.
- Run:
  - `./scripts/run-metadata-lock-scenario.sh mysql84 ./state/metadata-lock/mysql84`
  - or `./scripts/run-metadata-lock-scenario.sh mariadb11 ./state/metadata-lock/mariadb11`

What to inspect:
- `summary.json`: confirms whether queue amplification was observed.
- `processlist.txt`: shows the blocking transaction plus waiting DDL and queued reader.
- `metadata-locks.tsv`: preferred object-level evidence when `performance_schema.metadata_locks` is available.
- MariaDB note: if metadata-lock instrumentation is unavailable, use processlist evidence and consider enabling `metadata_lock_info` manually for rehearsal.

Operator decision path:
1. Confirm the waiting DDL is really blocked on metadata lock, not on row-lock contention.
2. Identify the blocking session and decide whether it is safer to drain/finish it or to kill the waiting DDL.
3. If ordinary reads/writes are already queueing behind the waiting DDL, abort the waiting DDL first to reduce blast radius.
4. Re-run schema change only during a drained window with a conservative `lock_wait_timeout`.

Current scope:
- This is operator guidance and rehearsal support, not automatic lock-killing behavior in `dbmigrate`.
- For MariaDB, deep metadata-lock visibility may still require manual plugin enablement depending on image/runtime defaults.

## Rollback strategy (to refine)

- Take source and destination backups before baseline migration.
- Keep replication checkpoints immutable per successful run.
- If verification fails, stop apply loop and inspect report before retry.

## Backup and restore rehearsal

Do not treat "backup completed" as evidence that rollback is safe.

Recommended rehearsal:
- Start one service with `docker compose up -d <service>`.
- Run:
  - `./scripts/run-backup-restore-rehearsal.sh mysql84 ./state/backup-restore/mysql84`
  - or `./scripts/run-backup-restore-rehearsal.sh mariadb11 ./state/backup-restore/mariadb11`

What the rehearsal distinguishes:
- `backup_completed=true`: the dump command succeeded and produced an artifact.
- `backup_validated=true`: the artifact contains the expected table, view, routine, and event definitions.
- `restore_usable=true`: the artifact restored into a shadow schema and passed smoke tests.

What to inspect:
- `logical-backup.sql`: the actual backup artifact
- `validation.txt`: whether the expected objects are present in the artifact
- `restore-smoke.txt`: whether restored rows, view access, routine execution, and event presence were verified
- `summary.json`: the machine-readable result for automation and runbooks

Operational rule:
- A release-grade rollback claim requires `restore_usable=true`, not just backup completion.
- Physical backup workflows remain a separate risk class and need their own vendor-supported prepare and restore procedures.

## Time-zone and `NOW()` rehearsal

Do not assume matching SQL text means matching temporal behavior.

Recommended rehearsal:
- Start one service with `docker compose up -d <service>`.
- Run:
  - `./scripts/run-timezone-rehearsal.sh mysql84 ./state/timezone/mysql84`
  - or `./scripts/run-timezone-rehearsal.sh mariadb11 ./state/timezone/mariadb11`

What this checks:
- whether `TIMESTAMP` renders differently under different session `time_zone` values
- whether `DATETIME` stays stable under the same session changes
- whether `NOW()`-driven inserts expose the semantic split clearly enough for operator review

What to inspect:
- `server-variables.txt`: baseline `system_time_zone`, global `time_zone`, and default session `time_zone`
- `query-utc.tsv` and `query-alt.tsv`: the same rows rendered under different session offsets
- `summary.json`: compact result for automation and runbooks

Operational rule:
- If the app or cutover path depends on local-time rendering, review `TIMESTAMP` and `DATETIME` usage explicitly before claiming compatibility.
- Prefer UTC discipline and explicit session initialization where possible.

## Plugin lifecycle and unsupported-engine rehearsal

Do not assume plugin and engine compatibility just because connectivity works.

Recommended rehearsal:
- Start the required services with `docker compose up -d <source-service> <dest-service>`.
- Run:
  - `./scripts/run-plugin-lifecycle-rehearsal.sh mysql80 mysql84 ./state/plugin-lifecycle/mysql80-to-mysql84`
  - or `./scripts/run-plugin-lifecycle-rehearsal.sh mariadb11 mysql84 ./state/plugin-lifecycle/mariadb11-to-mysql84`

What this checks:
- whether source account plugins are active on the destination
- whether the destination still exposes `@@default_authentication_plugin`
- whether selected source tables use engines unsupported on the destination
- whether `dbmigrate plan` surfaces the expected findings for the exact pair

What to inspect:
- `plan-output.json`: real `dbmigrate` compatibility output
- `source-accounts.tsv`: the accounts used for the rehearsal
- `source-table-engines.tsv`: the selected table-engine inventory
- `dest-plugins.tsv` and `dest-engines.tsv`: destination capability inventory
- `summary.json`: machine-readable summary for automation and runbooks

Operational rule:
- Treat unsupported storage engines as a migration blocker until tables are converted or excluded.
- Treat unsupported auth plugins as a required account-cutover task; do not auto-rewrite them blindly.

## Invisible-column and GIPK rehearsal

Do not assume a destination that parses the DDL also preserves the hidden-schema semantics.

Recommended rehearsal:
- Start the required services with `docker compose up -d <source-service> <dest-service>`.
- Run:
  - `./scripts/run-invisible-gipk-rehearsal.sh mysql84 mariadb10 ./state/invisible-gipk/mysql84-to-mariadb10`
  - `./scripts/run-invisible-gipk-rehearsal.sh mysql84 mysql80 ./state/invisible-gipk/mysql84-to-mysql80`
  - or `./scripts/run-invisible-gipk-rehearsal.sh mysql84 mariadb11 ./state/invisible-gipk/mysql84-to-mariadb11`

What this checks:
- whether invisible columns remain invisible after restore
- whether invisible indexes remain hidden after restore
- whether included dumps preserve generated invisible primary keys as hidden
- whether `--skip-generated-invisible-primary-key` removes the hidden PK from the logical schema
- whether `dbmigrate plan` blocks the destination pair when hidden-schema semantics drift

What to inspect:
- `source-show-create.txt`: source DDL evidence
- `dump-included.sql` and `dump-skipped.sql`: the two logical dump shapes
- `dest-included-show-create.txt` and `dest-skipped-show-create.txt`: destination DDL evidence after restore
- `plan-output.json`: real compatibility result for the exact pair
- `summary.json`: machine-readable verdict for preservation versus drift

Operational rule:
- MySQL -> MariaDB with invisible columns, invisible indexes, or GIPK is blocked by default because the destination materializes hidden schema as visible objects.
- MySQL -> MySQL is only acceptable when the destination line supports the hidden feature in question and the dump mode is intentional.

## Replication parallelism and transaction-shape rehearsal

Do not assume "more workers" fixes replication lag.

Recommended rehearsal:
- Start one service with `docker compose up -d <service>`.
- Run:
  - `./scripts/run-replication-shape-rehearsal.sh mysql84 ./state/replication-shape/mysql84`
  - or `./scripts/run-replication-shape-rehearsal.sh mariadb11 ./state/replication-shape/mariadb11`

What this checks:
- the same row volume written as one huge transaction versus many smaller commits
- FK-bound workload shape, which is a common serialization pressure point
- why commit boundaries matter more than nominal worker count

What to inspect:
- `summary.json`: transaction count, max rows per transaction, and comparison flags
- `monolithic.sql` versus `chunked.sql`: the actual transaction boundary difference
- `notes.txt`: operator interpretation for runbooks

Operational rule:
- If replication windows keep stalling at transaction boundaries, reduce source transaction size before you start tuning worker counts.
- Use checkpoint/report shape fields as evidence: `risk_signals` such as `large_transaction_dominates`, `ddl_serializes_apply`, `foreign_key_serialization_pressure`, and `keyless_row_matching_pressure` are the real warning signs.

## Temporary CI operations note (review later)

- If GitHub automatic workflow triggers are degraded, dispatch CI manually for a branch:
  - `make ci-manual`
  - or `make ci-manual BRANCH=<branch-name>`
- This is a temporary operational workaround.
- Review later: re-enable strict required status checks on `main` after automatic `push`/`pull_request` triggers are healthy again.
