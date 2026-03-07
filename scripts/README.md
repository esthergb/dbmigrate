# Migration Matrix Scripts

This directory contains executable wrappers for exhaustive migration scenario testing.

## Scope

- Legacy matrix: 20 scenarios across MariaDB 10.6/11.0/12.0 and MySQL 8.0/8.4.
- Frozen v1 matrix: exact release-grade services for MariaDB 10.11/11.4/11.8 and MySQL 8.4.
- Same-engine and cross-engine paths.

## Structure

- `run-migration-test.sh`: shared runner with robust orchestration.
- `run-compat-probes.sh`: compatibility probes executed on source/destination services before migration.
- `run-backup-restore-rehearsal.sh`: logical backup/restore rehearsal that distinguishes backup completion, validation, and restore usability.
- `run-timezone-rehearsal.sh`: local proof of session time-zone drift for `NOW()`, `TIMESTAMP`, and `DATETIME`.
- `run-metadata-lock-scenario.sh`: local reproduction of metadata-lock queue amplification with observability artifact capture.
- `run-plugin-lifecycle-rehearsal.sh`: local proof of auth-plugin drift and unsupported storage-engine detection.
- `run-replication-shape-rehearsal.sh`: local proof that transaction shape matters more than nominal worker count.
- `run-invisible-gipk-rehearsal.sh`: local proof of invisible-column visibility drift and generated invisible primary key dump behavior.
- `run-collation-rehearsal.sh`: local proof that server-unsupported collations and client-compatibility risk are different failure classes.
- `run-verify-canonicalization-rehearsal.sh`: local proof that naive hashes can drift while canonicalized verify remains stable.
- `test-*.sh`: legacy scenario wrappers that call the shared runner.
- `test-v1-*.sh`: frozen v1 matrix wrappers targeting the exact release-grade local service set.
- `test-v1-supplemental-*.sh`: extra upgrade-evidence wrappers requested outside the frozen strict-lts release lane.

## What the Runner Does

For each scenario, the runner performs:

1. reset environment (`docker compose down -v --remove-orphans`)
2. start source and destination services
3. wait for health checks with retries
4. prepare scenario state dir and execute compatibility probes on source and destination (writes JSON artifacts)
5. seed the **source** service from `datasets/` (destination remains empty)
6. keep probe artifacts in scenario state dir
7. run `plan`, `migrate`, `verify --data-mode count`, and `report --json`

## Prerequisites

1. Docker with Compose v2 (`docker compose`) available.
2. Local Go toolchain / build dependencies for `make build`.

Compatibility probe toggle:

- Enabled by default (`DBMIGRATE_ENABLE_COMPAT_PROBES=1`)
- Disable for faster smoke runs:

```bash
DBMIGRATE_ENABLE_COMPAT_PROBES=0 ./scripts/test-mariadb10-to-mariadb11.sh
```

Current probe coverage (`run-compat-probes.sh`):

- Engine/version compatibility deltas (`SET PERSIST`, JSON operators, `LATERAL`, timeout vars, auth plugin vars, FK behavior).
- Environment diagnostics (`character_set_server`, `collation_server`, `session sql_mode`).
- Invalid-default simulation probes (`Error 1067` family):
  - zero-date defaults (`0000-00-00`, `0000-00-00 00:00:00`, `YYYY-00-DD`)
  - invalid calendar defaults (`YYYY-02-30`)
  - out-of-range `TIMESTAMP` defaults (pre-1970)

## Quick Start

Run one scenario:

```bash
./scripts/test-mariadb10-to-mariadb11.sh
```

Run the Phase 57 metadata-lock rehearsal against a single service:

```bash
docker compose up -d mysql84
./scripts/run-metadata-lock-scenario.sh mysql84 ./state/metadata-lock/mysql84
```

```bash
docker compose up -d mariadb11
./scripts/run-metadata-lock-scenario.sh mariadb11 ./state/metadata-lock/mariadb11
```

Run the Phase 58 backup/restore rehearsal against a single service:

```bash
docker compose up -d mysql84
./scripts/run-backup-restore-rehearsal.sh mysql84 ./state/backup-restore/mysql84
```

```bash
docker compose up -d mariadb11
./scripts/run-backup-restore-rehearsal.sh mariadb11 ./state/backup-restore/mariadb11
```

Run the Phase 59 time-zone rehearsal against a single service:

```bash
docker compose up -d mysql84
./scripts/run-timezone-rehearsal.sh mysql84 ./state/timezone/mysql84
```

```bash
docker compose up -d mariadb11
./scripts/run-timezone-rehearsal.sh mariadb11 ./state/timezone/mariadb11
```

Run the Phase 60 plugin and engine lifecycle rehearsal against a source/destination pair:

```bash
docker compose up -d mysql80 mysql84
./scripts/run-plugin-lifecycle-rehearsal.sh mysql80 mysql84 ./state/plugin-lifecycle/mysql80-to-mysql84
```

```bash
docker compose up -d mariadb11 mysql84
./scripts/run-plugin-lifecycle-rehearsal.sh mariadb11 mysql84 ./state/plugin-lifecycle/mariadb11-to-mysql84
```

Run the Phase 61 replication transaction-shape rehearsal against a single service:

```bash
docker compose up -d mysql84
./scripts/run-replication-shape-rehearsal.sh mysql84 ./state/replication-shape/mysql84
```

```bash
docker compose up -d mariadb11
./scripts/run-replication-shape-rehearsal.sh mariadb11 ./state/replication-shape/mariadb11
```

Run the Phase 62 invisible-column and GIPK rehearsal against a source/destination pair:

```bash
docker compose up -d mysql84 mysql80
./scripts/run-invisible-gipk-rehearsal.sh mysql84 mysql80 ./state/invisible-gipk/mysql84-to-mysql80
```

```bash
docker compose up -d mysql84 mariadb11
./scripts/run-invisible-gipk-rehearsal.sh mysql84 mariadb11 ./state/invisible-gipk/mysql84-to-mariadb11
```

```bash
docker compose up -d mysql84 mariadb10
./scripts/run-invisible-gipk-rehearsal.sh mysql84 mariadb10 ./state/invisible-gipk/mysql84-to-mariadb10
```

Run the Phase 63 collation compatibility and client-risk rehearsal:

```bash
docker compose up -d mysql80 mysql84 mariadb10 mariadb12
./scripts/run-collation-rehearsal.sh ./state/collation-phase63
```

Run the Phase 64 verification canonicalization rehearsal:

```bash
docker compose up -d mysql84 mariadb12
./scripts/run-verify-canonicalization-rehearsal.sh ./state/verify-canonicalization-phase64
```

Run all legacy scenarios sequentially:

```bash
for script in scripts/test-*.sh; do
  echo "Running $script"
  bash "$script" || echo "FAILED: $script"
done
```

Run the frozen v1 matrix only:

```bash
./scripts/test-v1-matrix.sh
```

Frozen `v1` wrappers currently cover the exact release-grade strict-lts pairs only:

- `test-v1-mysql84-to-mysql84.sh`
- `test-v1-mariadb1011-to-mariadb1011.sh`
- `test-v1-mariadb114-to-mariadb114.sh`
- `test-v1-mariadb118-to-mariadb118.sh`
- `test-v1-mysql84-to-mariadb114.sh`
- `test-v1-mariadb114-to-mysql84.sh`

They intentionally do not include `max-compat` candidate paths or broader same-major range sweeps.

Supplemental wrappers currently cover:

- `test-v1-supplemental-mariadb1011-to-mariadb114.sh`
- `test-v1-supplemental-mariadb1011-to-mariadb118.sh`
- `test-v1-supplemental-mariadb114-to-mariadb118.sh`
- `test-v1-supplemental-mysql80-to-mysql84.sh`

Run them together with:

```bash
./scripts/test-v1-supplemental-matrix.sh
```

## Troubleshooting

Show service logs:

```bash
docker compose -f docker-compose.yml logs mariadb10
```

Check service status:

```bash
docker compose -f docker-compose.yml ps
```

Ping inside container:

```bash
docker compose -f docker-compose.yml exec -T mariadb10 mariadb-admin ping -h localhost -u root -prootpass123
```

## Expected Failures

Scenario failures are expected when source/destination engine/version combinations are incompatible.  
The runner always executes `report --json --fail-on-conflict=false`, so each failure keeps a structured artifact trail for analysis.

Compatibility probes intentionally include statements that fail on some engines/versions to surface known SQL/DDL behavior differences.

## Metadata-lock rehearsal artifacts

`run-metadata-lock-scenario.sh` is intentionally outside the main migration matrix. It is a focused operator rehearsal for the Phase 57 failure mode where a waiting DDL amplifies into blocked ordinary traffic.

Artifacts written to the chosen output directory:

- `summary.json`: machine-readable scenario summary
- `server-variables.txt`: relevant server settings
- `processlist.txt`: point-in-time `SHOW FULL PROCESSLIST` capture
- `metadata-locks.tsv`: `performance_schema.metadata_locks` capture when available
- `plugin-attempt.txt`: MariaDB plugin note for deeper lock visibility when relevant
- `blocker.log`, `ddl.log`, `read.log`: session-level evidence

Interpretation:

- `ddl_exit_code != 0` with `read_elapsed_seconds >= 2` is the expected strong signal for queue amplification.
- If `metadata_locks_available=false`, rely on `processlist.txt` and consider enabling MariaDB `metadata_lock_info` manually during rehearsal.

## Backup/restore rehearsal artifacts

`run-backup-restore-rehearsal.sh` is the focused Phase 58 rehearsal for the gap between "backup completed" and "restore is actually usable".

Artifacts written to the chosen output directory:

- `logical-backup.sql`: engine-native logical backup artifact
- `validation.txt`: artifact-level checks showing whether expected objects are present in the dump
- `restore-smoke.txt`: post-restore smoke test evidence
- `summary.json`: machine-readable distinction between backup completion, validation, and restore usability
- `summary.json` also records the exact dump client and server version used for the rehearsal

Interpretation:

- `backup_completed=true` only means the dump command succeeded.
- `backup_validated=true` means the artifact contains the expected object definitions.
- `restore_usable=true` means the artifact restored into a shadow schema and passed smoke tests.

## Verification canonicalization rehearsal artifacts

`run-verify-canonicalization-rehearsal.sh` is the focused Phase 64 rehearsal for noisy verify/checksum false positives.

Artifacts written to the chosen output directory:

- `source-*.tsv` and `dest-*.tsv`: raw cross-engine evidence showing why naive hashes differ
- `verify-hash.json`, `verify-sample.json`, `verify-full-hash.json`: canonicalized verify results
- `report.json`: final report output based on `verify-data-report.json`
- `summary.json`: compact scenario summary including naive-hash drift and canonical verify exit codes

Interpretation:

- `naive_hashes_differ=true` is expected in this rehearsal.
- `verify_hash_exit_code=0`, `verify_sample_exit_code=0`, and `verify_full_hash_exit_code=0` show the canonicalized verify path removed the false positive.
- `representation_risk_tables > 0` is also expected; it means the scenario is sensitive enough to deserve evidence retention.

Phase boundary:

- This script is intentionally logical and tool-native. It does not claim to validate physical backup portability across version lines.

## Time-zone rehearsal artifacts

`run-timezone-rehearsal.sh` is the focused Phase 59 rehearsal for session time-zone drift and `NOW()` semantics.

Artifacts written to the chosen output directory:

- `server-variables.txt`: `system_time_zone`, global `time_zone`, and default session `time_zone`
- `query-utc.tsv`: same rows observed under `+00:00`
- `query-alt.tsv`: same rows observed under `+02:00`
- `summary.json`: machine-readable statement of whether `TIMESTAMP` display changed while `DATETIME` stayed stable

Interpretation:

- `timestamp_display_changes=true` means the same stored `TIMESTAMP` rendered differently after a session time-zone change.
- `datetime_static_under_session_change=true` means the `DATETIME` value did not shift when the session time zone changed.
- `explicit_now_drift_visible=true` means `NOW()`-driven inserts exposed the expected `TIMESTAMP` versus `DATETIME` semantic split.

## Plugin lifecycle rehearsal artifacts

`run-plugin-lifecycle-rehearsal.sh` is the focused Phase 60 rehearsal for auth-plugin drift, removed default-auth assumptions, and unsupported storage engines.

Artifacts written to the chosen output directory:

- `source-accounts.tsv`: source fixture accounts and their auth plugins
- `source-table-engines.tsv`: selected source tables and their storage engines
- `dest-plugins.tsv`: destination active plugin inventory
- `dest-engines.tsv`: destination engine support inventory
- `dest-default-auth.txt`: destination `@@default_authentication_plugin` value, or `unavailable`
- `plan-output.json`: actual `dbmigrate plan` output for the pair
- `summary.json`: compact statement of whether unsupported auth plugins or storage engines were detected

Interpretation:

- `unsupported_auth_plugins_detected=true` means account plugins visible on source are not active on destination and must be normalized before user/grant cutover work.
- `unsupported_storage_engines_detected=true` means `plan` and schema `migrate` should fail fast before DDL apply.
- MySQL `8.4` intentionally reports `dest-default-auth.txt=unavailable` because `default_authentication_plugin` is no longer a usable variable there.

## Replication transaction-shape rehearsal artifacts

`run-replication-shape-rehearsal.sh` is the focused Phase 61 rehearsal for the gap between "more workers" and "actually parallelizable transaction shape".

Artifacts written to the chosen output directory:

- `setup.sql`: schema and FK setup used by the rehearsal
- `monolithic.sql`: one large transaction for the full workload
- `chunked.sql`: the same row volume split into many small commits
- `monolithic.log` and `chunked.log`: execution logs for each variant
- `notes.txt`: operator-facing interpretation
- `summary.json`: compact machine-readable comparison

Interpretation:

- `same_total_rows=true` with different transaction counts is the point: identical row volume can create very different replication scheduling pressure.
- `monolithic_dominates_transaction_shape=true` means one huge transaction would remain one serialization unit for replica-style apply.
- `chunked_reduces_commit_granularity=true` means smaller commits create more restart-safe and scheduler-friendly units, even before any worker setting is considered.

## Invisible-column and GIPK rehearsal artifacts

`run-invisible-gipk-rehearsal.sh` is the focused Phase 62 rehearsal for hidden-schema downgrade and cross-engine drift.

Artifacts written to the chosen output directory:

- `source-fixture.sql`: rendered Phase 62 source fixture
- `source-show-create.txt`: source `SHOW CREATE TABLE` evidence for the invisible-column and GIPK tables
- `source-columns.tsv` and `source-indexes.tsv`: source metadata inventory
- `dump-included.sql`: default logical dump preserving generated invisible primary keys
- `dump-skipped.sql`: logical dump produced with `--skip-generated-invisible-primary-key`
- `dest-included-show-create.txt` and `dest-skipped-show-create.txt`: destination `SHOW CREATE TABLE` evidence for both dump modes
- `dest-included-columns.tsv` and `dest-skipped-columns.tsv`: destination column inventory after restore
- `plan-output.json`: real `dbmigrate plan` output for the source/destination pair
- `summary.json`: compact machine-readable verdict for visibility drift and GIPK include/skip behavior

Interpretation:

- `included_invisible_column_preserved=true` means the destination kept the MySQL invisible-column semantic instead of silently materializing it as visible.
- `included_invisible_index_preserved=true` means the destination kept the invisible index hidden.
- `included_gipk_remains_invisible=true` means the included dump preserved the hidden primary key semantics.
- `skipped_gipk_column_present=false` means `--skip-generated-invisible-primary-key` removed the hidden key from the logical schema entirely.
- `visibility_drift_detected=true` means at least one source hidden-schema feature became visible or semantically different on restore.

## Collation compatibility and client-risk rehearsal artifacts

`run-collation-rehearsal.sh` is the focused Phase 63 rehearsal for separating server-side unsupported collations from client/library compatibility risk.

Artifacts written to the chosen output directory:

- `summary.json`: top-level index of the three fixed Phase 63 scenarios
- per-scenario `summary.json`: plan/restore/client-probe exit codes plus artifact paths
- `source-collations.tsv`: source schema/table/column collation inventory
- `plan-output.json`: actual `dbmigrate plan` output for that scenario
- `report-output.json`: `dbmigrate report` output reading `collation-precheck.json`
- `dump.sql`: logical dump artifact used for restore rehearsal
- `import.stderr.log`: direct restore failure evidence when destination server rejects the collation
- `client-probe.txt`: representative older-client query result against the target server

Scenario set:

- `mysql84 -> mariadb10` using `utf8mb4_0900_ai_ci`
- `mariadb12 -> mysql84` using `utf8mb4_uca1400_ai_ci`
- `mariadb12 -> mariadb12` using `utf8mb4_uca1400_ai_ci`

Interpretation:

- `plan_exit_code=2` with `restore_exit_code=1` means the collation is a real server-side incompatibility, not just a client quirk.
- `plan_exit_code=0` with `client_compatibility_risk_count > 0` means the server accepts the schema but representative application/client rehearsal is still required.
- `representative_client_probe_exit_code=0` means the tested CLI connected; that is useful evidence, but it is not proof that every production driver will behave the same way.
