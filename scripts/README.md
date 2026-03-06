# Migration Matrix Scripts

This directory contains executable wrappers for exhaustive migration scenario testing.

## Scope

- 20 scenarios across MariaDB 10.6/11.0/12.0 and MySQL 8.0/8.4.
- Same-engine and cross-engine paths.
- Upgrade and downgrade directions.

## Structure

- `run-migration-test.sh`: shared runner with robust orchestration.
- `run-compat-probes.sh`: compatibility probes executed on source/destination services before migration.
- `run-backup-restore-rehearsal.sh`: logical backup/restore rehearsal that distinguishes backup completion, validation, and restore usability.
- `run-timezone-rehearsal.sh`: local proof of session time-zone drift for `NOW()`, `TIMESTAMP`, and `DATETIME`.
- `run-metadata-lock-scenario.sh`: local reproduction of metadata-lock queue amplification with observability artifact capture.
- `test-*.sh`: thin scenario wrappers that call the shared runner.

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

Run all scenarios sequentially:

```bash
for script in scripts/test-*.sh; do
  echo "Running $script"
  bash "$script" || echo "FAILED: $script"
done
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
- `restore_usable=true` means the dump restored into a shadow schema and passed smoke tests for rows, views, routines, and event presence.

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
