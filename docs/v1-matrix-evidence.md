# dbmigrate v1 Matrix Evidence

## Purpose

This document records the current release-grade matrix execution evidence for `dbmigrate v1`.

It is an evidence artifact, not a support-policy document.
Use it together with:

- `README.md`
- `docs/v1-release-plan.md`
- `docs/v1-release-criteria.md`

## Test Context

- Tested revision: `297e989`
- Evidence timestamp (UTC): `2026-03-07T00:22:03Z`
- Execution branch: `codex/chore/v1-matrix-execution-phase71`
- Host context: local Apple Silicon workstation
- Container runtime: `docker compose`

## Commands Run

```bash
go test ./...
./scripts/test-v1-matrix.sh
./scripts/test-v1-supplemental-matrix.sh
```

## Classification Summary

### Frozen strict-lts v1 lane

- Scenarios run: `6`
- Supported and passed: `6`
- Blocked by design: `0`
- Unexpected regressions: `0`

### Supplemental lane

- Scenarios run: `4`
- Supplemental and passed: `4`
- Blocked by design: `0`
- Unexpected regressions: `0`

## Frozen strict-lts Results

| Scenario | Source service | Source version | Destination service | Destination version | Profile | plan | migrate | verify | report | Classification | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| MySQL 8.4 -> MySQL 8.4 | `mysql84a` | `8.4.8` | `mysql84b` | `8.4.8` | `strict-lts` | `0` | `0` | `0` | `0` | supported and passed | no incompatible findings |
| MariaDB 10.11 -> MariaDB 10.11 | `mariadb1011a` | `10.11.16-MariaDB-ubu2204` | `mariadb1011b` | `10.11.16-MariaDB-ubu2204` | `strict-lts` | `0` | `0` | `0` | `0` | supported and passed | no incompatible findings |
| MariaDB 11.4 -> MariaDB 11.4 | `mariadb114a` | `11.4.10-MariaDB-ubu2404` | `mariadb114b` | `11.4.10-MariaDB-ubu2404` | `strict-lts` | `0` | `0` | `0` | `0` | supported and passed | warning-only `uca1400` client-compatibility risk |
| MariaDB 11.8 -> MariaDB 11.8 | `mariadb118a` | `11.8.6-MariaDB-ubu2404` | `mariadb118b` | `11.8.6-MariaDB-ubu2404` | `strict-lts` | `0` | `0` | `0` | `0` | supported and passed | warning-only `uca1400` client-compatibility risk |
| MySQL 8.4 -> MariaDB 11.4 | `mysql84a` | `8.4.8` | `mariadb114b` | `11.4.10-MariaDB-ubu2404` | `strict-lts` | `0` | `0` | `0` | `0` | supported and passed | strict-lts cross-engine matrix match; warning-only auth-plugin drift and `uca1400` client risk |
| MariaDB 11.4 -> MySQL 8.4 | `mariadb114a` | `11.4.10-MariaDB-ubu2404` | `mysql84b` | `8.4.8` | `strict-lts` | `0` | `0` | `0` | `0` | supported and passed | strict-lts cross-engine matrix match; warning-only auth-plugin drift and source-side `uca1400` client risk |

## Supplemental Results

These scenarios are included as current evidence, but they are not part of the frozen strict-lts release lane.

| Scenario | Source service | Source version | Destination service | Destination version | Profile | plan | migrate | verify | report | Classification | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| MariaDB 10.11 -> MariaDB 11.4 | `mariadb1011a` | `10.11.16-MariaDB-ubu2204` | `mariadb114b` | `11.4.10-MariaDB-ubu2404` | `max-compat` | `0` | `0` | `0` | `0` | supplemental and passed | warning-only destination `uca1400` client risk |
| MariaDB 10.11 -> MariaDB 11.8 | `mariadb1011a` | `10.11.16-MariaDB-ubu2204` | `mariadb118b` | `11.8.6-MariaDB-ubu2404` | `max-compat` | `0` | `0` | `0` | `0` | supplemental and passed | warning-only destination `uca1400` client risk |
| MariaDB 11.4 -> MariaDB 11.8 | `mariadb114a` | `11.4.10-MariaDB-ubu2404` | `mariadb118b` | `11.8.6-MariaDB-ubu2404` | `same-major` | `0` | `0` | `0` | `0` | supplemental and passed | warning-only source/destination `uca1400` client risk |
| MySQL 8.0 -> MySQL 8.4 | `mysql80a` | `8.0.45` | `mysql84b` | `8.4.8` | `max-compat` | `0` | `0` | `0` | `0` | supplemental and passed | legacy upgrade evidence, not strict-lts |

## Key Observations

1. The frozen strict-lts lane completed with no regressions.
2. Both strict-lts cross-engine pairs passed end to end on current code.
3. Cross-engine success still carries warning-only auth-plugin drift. That is expected under the current product boundary because account execution is not treated as baseline-schema incompatibility.
4. MariaDB 11.4/11.8 paths surface warning-only `utf8mb4_uca1400_ai_ci` client-compatibility risk. Server-side migration succeeded; this remains an application-stack caveat, not a server-side blocker.
5. Supplemental scenarios remained clean under the selected non-strict-lts profiles (`max-compat` and `same-major`).

## Artifact Paths

The matrix runner writes detailed local artifacts under ignored runtime state directories.

Frozen lane artifacts:

- `state/v1/mysql84a-to-mysql84b/`
- `state/v1/mariadb1011a-to-mariadb1011b/`
- `state/v1/mariadb114a-to-mariadb114b/`
- `state/v1/mariadb118a-to-mariadb118b/`
- `state/v1/mysql84a-to-mariadb114b/`
- `state/v1/mariadb114a-to-mysql84b/`

Supplemental artifacts:

- `state/v1-supplemental/mariadb1011a-to-mariadb114b/`
- `state/v1-supplemental/mariadb1011a-to-mariadb118b/`
- `state/v1-supplemental/mariadb114a-to-mariadb118b/`
- `state/v1-supplemental/mysql80a-to-mysql84b/`

Each scenario directory contains at least:

- `compat-probes-source.json`
- `compat-probes-dest.json`
- `collation-precheck.json`
- `data-baseline-checkpoint.json`
- `verify-data-report.json`

## What This Does Not Prove Yet

This document does not complete `v1` signoff by itself.

Open release-criteria work still remains:

- rerun and archive the focused rehearsals listed in `docs/v1-release-criteria.md`
- attach the final release decision (`approved`, `blocked`, or `approved with caveats`)

## Current Signoff Read

Current read based on matrix evidence only:

- frozen strict-lts matrix: green
- supplemental matrix: green
- release signoff: not complete yet because focused rehearsal evidence still needs fresh release-grade archiving
