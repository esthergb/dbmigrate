# Migration Matrix Scripts

This directory contains executable wrappers for exhaustive migration scenario testing.

## Scope

- 20 scenarios across MariaDB 10.6/11.0/12.0 and MySQL 8.0/8.4.
- Same-engine and cross-engine paths.
- Upgrade and downgrade directions.

## Structure

- `run-migration-test.sh`: shared runner with robust orchestration.
- `test-*.sh`: thin scenario wrappers that call the shared runner.

## What the Runner Does

For each scenario, the runner performs:

1. reset environment (`docker compose down -v --remove-orphans`)
2. start source and destination services
3. wait for health checks with retries
4. seed the **source** service from `datasets/` (destination remains empty)
5. clear scenario state dir
6. run `plan`, `migrate`, `verify --data-mode count`, and `report --json`

## Prerequisites

1. Docker with Compose v2 (`docker compose`) available.
2. Local Go toolchain / build dependencies for `make build`.

## Quick Start

Run one scenario:

```bash
./scripts/test-mariadb10-to-mariadb11.sh
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
