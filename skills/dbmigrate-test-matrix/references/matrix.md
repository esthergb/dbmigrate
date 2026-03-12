# Validation matrix

## Frozen v1 strict-lts pairs

| Source | Destination | Profile | Docker services |
| ------ | ----------- | ------- | --------------- |
| MySQL 8.4 | MySQL 8.4 | strict-lts | mysql84a → mysql84b |
| MariaDB 10.11 | MariaDB 10.11 | strict-lts | mariadb1011a → mariadb1011b |
| MariaDB 11.4 | MariaDB 11.4 | strict-lts | mariadb114a → mariadb114b |
| MariaDB 11.8 | MariaDB 11.8 | strict-lts | mariadb118a → mariadb118b |
| MariaDB 11.4 | MySQL 8.4 | strict-lts | mariadb114a → mysql84a |
| MySQL 8.4 | MariaDB 11.4 | strict-lts | mysql84a → mariadb114a |

## Supplemental v1 pairs (same-major / adjacent-minor)

| Source | Destination | Profile |
| ------ | ----------- | ------- |
| MariaDB 10.11 | MariaDB 11.4 | adjacent-minor |
| MariaDB 10.11 | MariaDB 11.8 | adjacent-minor |
| MariaDB 11.4 | MariaDB 11.8 | adjacent-minor |
| MySQL 8.0 | MySQL 8.4 | same-major |

## Scenario groups

1. Baseline migration only (schema + data)
2. Baseline + incremental binlog replication
3. Baseline + replication + verification (all 4 modes)
4. Negative checks (intentional mismatches/conflicts)
5. Precheck validation (incompatible pairs blocked by design)

## Integration test scripts

Scripts live in `scripts/` with naming pattern `test-v1-<source>-to-<dest>.sh`.
Config files in `configs/` with matching names.
Test data in `testdata/` with SQL seed files.

## Local full-run recommendation

Run full groups in sequence and preserve logs by pair:
- `logs/<pair>/smoke.log`
- `logs/<pair>/full.log`

## v2 expansion targets

When v2 features land, add matrix coverage for:
- Routines/triggers/events migration across all v1 pairs.
- Trigger-CDC replication mode (requires binlog disabled on source).
- Hybrid replication mode (mixed binlog + trigger-CDC).
- GTID-based start position.
- User/grant migration with auth plugin incompatibilities.
