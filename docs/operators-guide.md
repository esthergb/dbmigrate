# Operators Guide (Draft)

## Intended workflow

1. Run `dbmigrate plan` and review compatibility warnings.
2. Run baseline migration (`dbmigrate migrate`).
3. Run periodic incremental sync (`dbmigrate replicate`).
4. Run verification (`dbmigrate verify`).
5. Generate machine-readable report (`dbmigrate report --json`).

Current v1 scope guardrails:
- Default object scope is `--include-objects=tables,views`.
- Requesting `routines`, `triggers`, or `events` fails fast in `v1` (reserved for `v2`).
- `--idempotent` is reserved for `v2` and currently fails fast in `v1`.
- Default transport policy is `--tls-mode=required`; using `--tls-mode=preferred` is explicit opt-in and warns that plaintext fallback is allowed.
- Global `--operation-timeout=<duration>` can bound end-to-end runtime for long commands; `0` disables the deadline.

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

Collation compatibility precheck:
- `plan` inventories selected source schema, table, and column collations plus source/destination server defaults.
- Schema `migrate` runs the same precheck before DDL apply.
- Server-unsupported collations fail fast before schema apply.
- Client/library compatibility risk remains warning-level when the server accepts the collation but the application stack may lag behind it.
- `report` loads `collation-precheck.json` and keeps these two classes separate:
  - `unsupported_destination_count`
  - `client_compatibility_risk_count`

Invisible-column and GIPK precheck:
- `plan` inventories source invisible columns, invisible indexes, and generated invisible primary key tables.
- Schema `migrate` runs the same precheck before DDL apply.
- MySQL -> MariaDB paths with these hidden-schema features fail fast because local rehearsal evidence shows semantic drift: hidden columns and indexes become visible, and included GIPKs stop being hidden.
- Compatible MySQL downgrade paths are still reported explicitly so operators keep dump mode intentional:
  - default dump preserves GIPK on MySQL targets that support it
  - `--skip-generated-invisible-primary-key` removes the hidden key from logical dumps entirely

Foreign-key cycle precheck:
- `plan` inventories intra-database foreign-key dependencies and fails when a cycle group is detected.
- Schema `migrate` and dry-run sandbox validation fail on the same cycle groups.
- v1 remediation is manual and explicit:
  - create/load the affected tables without the cyclic constraints
  - apply the cyclic `ALTER TABLE ... ADD CONSTRAINT ...` statements as a post-step

Schema-feature precheck:
- `plan` inventories documented schema-feature incompatibilities that are easy to miss in cross-engine lanes:
  - JSON columns on cross-engine paths
  - MariaDB `SEQUENCE` objects
  - MariaDB system-versioned tables
- `migrate` fails on the same incompatible feature classes before schema apply.
- Current v1 policy:
  - cross-engine JSON columns are treated as incompatible
  - MariaDB sequences are incompatible on non-MariaDB destinations
  - MariaDB system-versioned tables are incompatible on non-MariaDB destinations
  - MariaDB-only versioned tables still emit warnings on MariaDB destinations because replay semantics deserve separate rehearsal

Identifier portability precheck:
- `plan` inventories identifier/parser portability hazards before schema apply:
  - identifiers that collide with the destination reserved-word set
  - `lower_case_table_names` mismatch between source and destination
  - case-fold collisions (`Orders` vs `orders`)
  - mixed-case database/table/view names when either side applies case folding
  - view definitions that depend on SQL-mode parser behavior (`ANSI_QUOTES`, `PIPES_AS_CONCAT`, `NO_BACKSLASH_ESCAPES`)
- `migrate` fails on the same incompatible identifier-portability findings before schema apply.
- v1 remediation is explicit:
  - rename identifiers that become newly reserved on the destination
  - keep already-reserved identifiers quoted consistently and treat them as warning-level portability debt
  - normalize mixed-case names before moving across case-folding boundaries
  - rewrite parser-sensitive views or align SQL-mode semantics before cutover

Replication boundary inventory:
- `plan` inventories cross-engine continuity boundary state even though `--start-from=gtid` remains reserved for `v2`.
- Current inventory includes:
  - source/destination GTID state
  - source `log_bin`
  - source `binlog_format`
  - MySQL -> MariaDB `binlog_row_value_options`
  - MySQL -> MariaDB `binlog_transaction_compression`
- v1 interpretation:
  - cross-engine GTID auto-position is not a supported resume contract
  - file/position remains the only supported cross-engine continuity boundary
  - boundary findings stay warning-level so baseline-only migrations are not blocked by replication-only inventory

Replication readiness precheck:
- `plan` now inventories the same source-side replication readiness settings that `replicate` enforces at runtime:
  - `log_bin`
  - `binlog_format`
  - `binlog_row_image`
  - current binary log status / file-position handoff when visible
- Current v1 policy:
  - these findings stay warning-level in `plan`
  - `replicate` remains the hard gate and still fails if `log_bin` is off, `binlog_format != ROW`, or `binlog_row_image != FULL`
  - use the `plan` findings to fix topology readiness before the first incremental rehearsal

Temporal/time-zone portability precheck:
- `plan` now records:
  - source/destination `system_time_zone`
  - source/destination global/session `time_zone`
  - tables containing `TIMESTAMP` and `DATETIME`
  - tables that mix both temporal models
- Current v1 policy:
  - these findings are warning-level portability inventory
  - they do not prove or disprove application correctness on their own
  - they are meant to force explicit review before claiming temporal compatibility across environments

Data-shape precheck:
- `plan` now inventories:
  - tables without a primary key or non-null unique key
  - representation-sensitive tables with approximate numerics, temporal columns, JSON, or collation-sensitive text
- `migrate` now fails before baseline data copy when selected scope includes keyless tables.
- v1 interpretation:
  - keyless tables are incompatible for live baseline and deterministic verify modes
  - representation-sensitive tables stay warning-level but should drive canonicalized verify and rehearsal choices

Manual-evidence findings:
- `plan` now emits explicit warning-level findings for documented operational classes that metadata alone cannot certify:
  - backup/restore usability evidence
  - metadata-lock runbook readiness
  - transaction-shape rehearsal
  - dump/import tool skew review
  - view-definer rewrite review when views are in scope
  - source CURRENT_USER() grant inventory hints for replication workflows
- These findings are not noise. They are the line between “schema looks compatible” and “cutover is actually rehearsed.”

## Baseline migration execution

- Schema-only:
  - `dbmigrate migrate --source "<dsn>" --dest "<dsn>" --schema-only`
- Data-only:
  - `dbmigrate migrate --source "<dsn>" --dest "<dsn>" --data-only --chunk-size 1000`
- Full baseline:
  - `dbmigrate migrate --source "<dsn>" --dest "<dsn>" --chunk-size 1000`

Checkpoint and resume:
- Baseline data copy writes checkpoint state into `--state-dir` (default `./state`).
- `dbmigrate` enforces a single-writer lock per `--state-dir`; concurrent `plan`/`migrate`/`replicate`/`verify` runs against the same state directory fail fast instead of racing checkpoint/report artifacts.
- If a crash leaves `.dbmigrate.lock` behind, v1 recovery is manual by design:
  - verify no active dbmigrate process still owns that `--state-dir`
  - remove the stale lock file
  - rerun the command
- Use `--resume` to continue from checkpoint state after interruption.
- Baseline uses consistent source snapshot reads plus keyset pagination (stable PK/unique key order), with typed resume cursors from checkpoint state (`last_key_typed`, legacy `last_key` load-compatible).
- For live baselines, tables without primary key or non-null unique key fail fast as incompatible in `v1`.
- Dry-run sandbox DML validation uses the same stable-key/keyset contract; keyless tables fail fast instead of being “validated” with unstable offset scans.
- Baseline checkpoint artifacts include source watermark (`SHOW MASTER STATUS`/`SHOW BINARY LOG STATUS`) for baseline->replicate continuity evidence.
- Schema apply sanitizes source view `DEFINER=` clauses to `DEFINER=CURRENT_USER` to reduce cross-environment definer breakage.
- Intra-database foreign-key cycles are not auto-rewritten in `v1`; baseline requires a manual post-step for cyclic constraints.

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
- `sample` mode hashes only bounded rows (`--sample-size`) and is intended for fast triage.
- `sample`, `hash`, and `full-hash` require a stable table order (primary key or non-null unique key) and fail fast when absent.
- `hash` mode performs full-table deterministic hash with bounded memory (chunked streaming).
- `full-hash` mode performs full-table deterministic hash with stricter chunked streaming aggregation (not an alias of `hash`).
- Hash-based verification now canonicalizes rows before hashing:
  - deterministic ordering uses stable key order where required
  - verify sessions are pinned and normalized to `SET NAMES utf8mb4` plus `time_zone='+00:00'`
  - JSON payloads are canonicalized before hashing
- Verify JSON/text output now distinguishes representation-sensitive tables from real diffs:
  - `noise_risk_mismatches`
  - `representation_risk_tables`
  - per-table `table_risk` notes
- `dbmigrate report` loads `verify-data-report.json` when present:
  - real verify diffs escalate report status to `attention_required`
  - warning-only representation-risk tables keep report status `ok`
  - proposals explain when to trust `count`, `hash`, `sample`, and `full-hash`

## Verification canonicalization rehearsal

Do not trust naive checksums across engines when collation order, session time zone, JSON formatting, or approximate numerics differ.

Recommended rehearsal:
- Start the required services with `docker compose up -d mysql84 mariadb12`.
- Run:
  - `./scripts/run-verify-canonicalization-rehearsal.sh ./state/verify-canonicalization-phase64`

What this proves:
- naive cross-engine evidence can drift even when rows are semantically equivalent
- canonicalized `hash`, `sample`, and `full-hash` can still pass when row order, JSON key order, and temporal rendering differ
- `report` stays `ok` when there are representation-sensitive tables but no verify diffs

What to inspect:
- `summary.json`: compact scenario result with naive-hash drift versus canonical verify exit codes
- `verify-hash.json`, `verify-sample.json`, `verify-full-hash.json`: verify artifacts and canonicalization metadata
- `report.json`: final report status plus operator proposals
- `source-*.tsv` and `dest-*.tsv`: raw rehearsal evidence showing why naive hashing is noisy

Operational rule:
- Treat `count` as the lowest-confidence guardrail.
- Treat `sample` as a fast triage tool, not final proof.
- Treat `full-hash` plus clean canonicalization metadata as the strongest current data-verify signal.
- If verify passes but representation-sensitive tables exist, keep the artifact as evidence instead of claiming byte-for-byte identity.

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
- Destination checkpoint state is stored in table `dbmigrate_replication_checkpoint` and written atomically in the same destination transaction as applied row changes.
- Optional: set `--source-server-id=<n>` (`1..4294967295`) when running multiple replication workers against the same source to avoid `server_id` collisions.
- Row-based binlog events are decoded into SQL apply batches (insert upsert, update, delete) with commit-boundary checkpointing.
- Mixed DDL + row-event windows fail fast in `v1`; split windows at DDL boundaries and run `migrate --schema-only` before replaying adjacent row batches.
- Source-window buffering during binlog read is bounded by safety limits (events + estimated bytes); oversized windows fail fast as `source_window_buffer_limit_exceeded`.
- Keyless `UPDATE`/`DELETE` replay is blocked as unsafe and fails fast with remediation guidance.
- `--apply-ddl=apply` is safety-classified: risky DDL (drop/rename/destructive alter patterns) is blocked with remediation guidance.
- Source preflight gates: `log_bin=ON`, `binlog_format=ROW`, and `binlog_row_image=FULL`.
- Conflict report JSON includes categorized `failure_type` values (for example `schema_drift`, `conflict_duplicate_key`), `sql_error_code` when surfaced by the destination engine, and contextual samples: `value_sample`, `old_row_sample`, `new_row_sample`, `row_diff_sample`.
- Conflict report samples are redacted by default; plain-text samples require explicit `--conflict-values=plain`.
- `--start-file` must be a bare binlog filename; path-like values and invalid characters fail before replication starts.

## Report generation

- Generate machine-readable report from state artifacts:
  - `dbmigrate report --state-dir ./state --json`
- Include sensitive conflict samples in output only when explicitly needed:
  - `dbmigrate report --state-dir ./state --json --include-sensitive-artifacts`
- Default behavior is fail-fast when conflicts are present (`status=attention_required`), returning non-zero exit.
- Optional override to keep reporting but not fail the command:
  - `dbmigrate report --state-dir ./state --json --fail-on-conflict=false`
- Report scans these files when present:
  - `data-baseline-checkpoint.json`
  - `replication-checkpoint.json`
  - `replication-conflict-report.json`
- Report status values:
  - `ok`: no conflict failure reported.
  - `attention_required`: replication conflict report contains a failure, or an incompatible precheck artifact is present in `--state-dir`.
  - `empty`: no known artifacts found in `--state-dir`.
- `proposals` includes remediation guidance from the conflict report to help triage and rerun planning.
- Report marks conflict sample handling explicitly (redacted default vs plain opt-in).
- Default `report` exit behavior is now consistent with that status:
  - incompatible precheck artifacts fail the command by default
  - warning-only artifacts remain reportable without non-zero exit

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

## Collation compatibility and client-risk rehearsal

Do not collapse "destination server cannot load this schema" and "some clients may break later" into the same problem.

Recommended rehearsal:
- Start the required services with `docker compose up -d mysql80 mysql84 mariadb10 mariadb12`.
- Run:
  - `./scripts/run-collation-rehearsal.sh ./state/collation-phase63`

What this checks:
- MySQL `utf8mb4_0900_ai_ci` objects against a MariaDB 10.6 destination.
- MariaDB `utf8mb4_uca1400_ai_ci` objects against a MySQL 8.4 destination.
- MariaDB `utf8mb4_uca1400_ai_ci` objects against a MariaDB 12 destination where the server accepts them but older clients may lag.
- Representative cross-client probes so operator evidence does not stop at server-side DDL behavior.

Observed local evidence for this phase:
- `mysql84 -> mariadb10` with `utf8mb4_0900_ai_ci`:
  - `plan_exit_code=2`
  - `restore_exit_code=1`
  - restore failed with `ERROR 1273 ... Unknown collation: 'utf8mb4_0900_ai_ci'`
  - representative `mariadb10` CLI still connected to MySQL 8.4 and read `@@collation_server=utf8mb4_0900_ai_ci`
- `mariadb12 -> mysql84` with `utf8mb4_uca1400_ai_ci`:
  - `plan_exit_code=2`
  - `restore_exit_code=1`
  - restore failed with `ERROR 1273 ... Unknown collation: 'utf8mb4_uca1400_ai_ci'`
  - representative `mysql80` CLI still connected to MariaDB 12 and read `@@collation_server=utf8mb4_uca1400_ai_ci`
- `mariadb12 -> mariadb12` with `utf8mb4_uca1400_ai_ci`:
  - `plan_exit_code=0`
  - `restore_exit_code=0`
  - `client_compatibility_risk_count=7`
  - representative `mysql80` CLI still connected successfully, so the risk remains application-stack specific rather than universal CLI breakage

What to inspect:
- `plan-output.json`: exact `dbmigrate plan` findings
- `report-output.json`: post-plan artifact summary with separate server/client proposals
- `import.stderr.log`: direct logical-restore failure when the destination server does not support the collation
- `client-probe.txt`: representative older-client query result against the target server
- `source-collations.tsv`: source schema/table/column collation inventory
- `summary.json`: scenario-level machine-readable result

Operational rule:
- Treat `unsupported_destination_count > 0` as a schema blocker.
- Treat `client_compatibility_risk_count > 0` as a cutover gate for application/client rehearsal, not as proof that the server-side schema is invalid.

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
