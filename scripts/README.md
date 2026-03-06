# Migration Matrix Scripts

This directory contains executable wrappers for exhaustive migration scenario testing.

## Scope

- 20 scenarios across MariaDB 10.6/11.0/12.0 and MySQL 8.0/8.4.
- Same-engine and cross-engine paths.
- Upgrade and downgrade directions.

## Structure

- `run-migration-test.sh`: shared runner with robust orchestration.
- `run-compat-probes.sh`: compatibility probes executed on source/destination services before migration.
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
