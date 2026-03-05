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
- Compatibility probes are enabled by default and can be disabled with `DBMIGRATE_ENABLE_COMPAT_PROBES=0`.

## Scenario Execution Model

Each `scripts/test-*.sh` wrapper delegates to `scripts/run-migration-test.sh`, which:

1. resets containers and volumes for deterministic runs
2. starts source and destination services
3. waits until both are reachable (engine-specific ping with retries)
4. prepares scenario state directory and runs compatibility probes on both services
5. seeds the source service from the matching dataset file
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

## Latest Detailed Runs (2026-03-05)

Executed in this phase:

1. Compatibility probe validation on all engines:

```bash
for svc in mariadb10 mariadb11 mariadb12 mysql80 mysql84; do
  bash scripts/run-compat-probes.sh "$svc" "state/probe-validation/${svc}.json"
done
```

2. Full migration matrix with integrated probes:

```bash
for script in scripts/test-*.sh; do
  bash "$script"
done
```

Results summary:

- Matrix scenarios: `20/20` passed (`reports/matrix-phase56/20260305T224136Z/summary.tsv`).
- Probe pack: `20` probes per engine/service.
- New simulated `Error 1067` class probes:
  - zero-date defaults: `zero_datetime_default_strict`, `zero_timestamp_default_strict`, `zero_date_default_strict`, `zero_in_date_default_strict`
  - invalid temporal defaults: `invalid_calendar_date_default`, `invalid_calendar_datetime_default`, `timestamp_out_of_range_default`
- Outcome pattern:
  - MariaDB (`10.6/11.0/12.0`): zero-date default probes pass.
  - MySQL (`8.0/8.4`): zero-date default probes fail under default strict SQL mode.
  - Invalid calendar/range defaults fail in both engines.

## Notes

- Scripts are fail-fast (`set -euo pipefail`).
- Compatibility probes include engine/version-specific SQL and are expected to produce mixed pass/fail probe results.
- `plan` incompatibility exits are intentional signals and should be captured in the detailed matrix report.
- State artifacts are written under `state/<scenario>/`.
- Runner always attempts `report --json --fail-on-conflict=false`, even when `plan`, `migrate`, or `verify` fails.
- Foreign-key dependency ordering for schema and baseline data copy is implemented in core `migrate`.
- Remaining failures should be treated as compatibility findings (engine/version features), not harness failures.

## Artifacts to Review

- `state/<scenario>/compat-probes-source.json`
- `state/<scenario>/compat-probes-dest.json`
- `state/<scenario>/data-baseline-checkpoint.json`
- `state/<scenario>/replication-checkpoint.json` (if produced)
- `state/<scenario>/replication-conflict-report.json` (if produced)
