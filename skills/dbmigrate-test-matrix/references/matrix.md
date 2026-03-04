# Suggested validation matrix

## Engine/version targets

- MariaDB 10.9 -> MariaDB 11.x
- MariaDB 10.9 -> MariaDB 12.x
- MySQL 8.0 -> MySQL 8.4
- MariaDB 10.9 -> MySQL 8.x
- MySQL 8.x -> MariaDB 11.x/12.x

## Scenario groups

1. Baseline migration only
2. Baseline + incremental replication
3. Baseline + replication + verification full
4. Negative checks (intentional mismatches/conflicts)

## Local full-run recommendation

Run full groups in sequence and preserve logs by pair:
- `logs/<pair>/smoke.log`
- `logs/<pair>/full.log`
