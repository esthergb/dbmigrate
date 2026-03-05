# dbmigrate Migration Testing Guide

## Purpose

This guide covers exhaustive local validation of migration behavior across all configured engine/version combinations.

## Included Assets

- `docker-compose.yml`: local database services for matrix testing.
- `configs/`: 20 scenario configs (`source`, `dest`, state dir, profile, filters).
- `datasets/`: version-specific SQL datasets used to seed source services.
- `scripts/`: scenario wrappers plus shared runner.

## Runtime Requirements

- Docker Desktop / Docker Engine with Compose v2 (`docker compose`).
- Host architecture: Apple Silicon is supported by the selected official images.
- Go toolchain for building `dbmigrate` (`make build` or script auto-build).

## Scenario Execution Model

Each `scripts/test-*.sh` wrapper delegates to `scripts/run-migration-test.sh`, which:

1. resets containers and volumes for deterministic runs
2. starts source and destination services
3. waits until both are reachable (engine-specific ping with retries)
4. seeds the source service from the matching dataset file
5. clears the scenario state directory
6. executes:
   - `dbmigrate plan --config ...`
   - `dbmigrate migrate --config ...`
   - `dbmigrate verify --config ... --verify-level data --data-mode count`
   - `dbmigrate report --config ... --json`

## Run Commands

Single scenario:

```bash
./scripts/test-mariadb10-to-mariadb11.sh
```

Full matrix:

```bash
for script in scripts/test-*.sh; do
  echo "Testing $script"
  bash "$script" || echo "FAILED: $script"
done
```

## Notes

- Scripts are fail-fast (`set -euo pipefail`).
- `plan` incompatibility exits are intentional signals and should be captured in the detailed matrix report.
- State artifacts are written under `state/<scenario>/`.
- Runner always attempts `report --json --fail-on-conflict=false`, even when `plan`, `migrate`, or `verify` fails.
- Foreign-key dependency ordering for schema and baseline data copy is implemented in core `migrate`.
- Remaining failures should be treated as compatibility findings (engine/version features), not harness failures.

## Artifacts to Review

- `state/<scenario>/data-baseline-checkpoint.json`
- `state/<scenario>/replication-checkpoint.json` (if produced)
- `state/<scenario>/replication-conflict-report.json` (if produced)
