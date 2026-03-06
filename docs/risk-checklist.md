# Operator Risk Checklist (Preflight)

Last reviewed: 2026-03-06

Use this checklist before running `dbmigrate plan`, `migrate`, or `replicate`.

## A) Scope and topology

- [ ] Confirm source/destination engine and version pair is explicitly supported for this run.
- [ ] Confirm migration mode (same-engine upgrade/downgrade vs cross-engine) and expected constraints.
- [ ] Confirm database/object include-exclude lists are final.

## B) Access and privileges

- [ ] Source account has metadata read privileges (`information_schema`, object DDL read).
- [ ] Source account can read binlog for incremental mode.
- [ ] Destination account can create/alter/drop objects and write data.
- [ ] If user/grant migration is enabled: account has privilege to read and apply account/grant metadata.

## C) Replication readiness

- [ ] `log_bin` is enabled on source.
- [ ] `binlog_format=ROW` (preferred/required for safe incremental behavior).
- [ ] Cross-engine path validated for GTID compatibility expectations.
- [ ] If cross-engine GTID is incompatible: file/position start point prepared.
- [ ] For MySQL 8.0 -> MariaDB paths, verify required version/settings compatibility from MariaDB docs.

Reference:

- https://mariadb.com/docs/release-notes/community-server/about/compatibility-and-differences/replication-compatibility-between-mariadb-and-mysql
- https://dev.mysql.com/doc/refman/8.0/en/binary-log-formats.html

## D) Charset and collation

- [ ] Capture server/database/table/column charset and collation inventories.
- [ ] Check for unsupported destination collations.
- [ ] Check for mixed/cross-collation expression risk in routines/views.
- [ ] Define mapping/normalization policy before migration.

Reference:

- https://dev.mysql.com/doc/refman/8.4/en/charset-server.html
- https://dev.mysql.com/doc/refman/8.4/en/charset-collation-coercibility.html
- https://mariadb.com/docs/server/reference/sql-structure/sql-language-structure/reserved-words

## E) SQL compatibility

- [ ] Scan for reserved-word identifier collisions on destination version.
- [ ] Scan for engine-specific constructs (sequences, temporal system-versioning, unsupported syntax).
- [ ] Decide handling policy for incompatible objects (block, rewrite, manual).

Reference:

- https://dev.mysql.com/doc/refman/8.0/en/keywords.html
- https://mariadb.com/docs/release-notes/community-server/about/compatibility-and-differences/incompatibilities-and-feature-differences-between-mariadb-and-mysql-unmaint/incompatibilities-and-feature-differences-between-mariadb-11-0-and-mysql-8
- https://mariadb.com/docs/server/reference/sql-structure/sequences/create-sequence

## F) Authentication and account migration

- [ ] Inventory source auth plugins per user.
- [ ] Verify destination plugin support and defaults.
- [ ] Decide scope: business accounts only vs include system accounts.
- [ ] Decide password/plugin rewrite and account lock/reset workflow for incompatible users.
- [ ] Ensure TLS/RSA requirements for auth plugins are satisfied.

Reference:

- https://dev.mysql.com/doc/refman/8.4/en/caching-sha2-pluggable-authentication.html
- https://dev.mysql.com/doc/refman/8.4/en/native-pluggable-authentication.html
- https://dev.mysql.com/doc/relnotes/mysql/8.4/en/news-8-4-0.html
- https://mariadb.com/docs/server/security/user-account-management/authentication-from-mariadb-10-4

## G) Data consistency and verification

- [ ] Pick verification level (`schema`, `data`, `full`) and data mode (`count`, `hash`, `sample`, `full-hash`).
- [ ] Define tolerated differences (`definer`, `auto_increment`, table options, collation diffs).
- [ ] Define pass/fail criteria and exit-code handling for automation.

## H) Operational safety

- [ ] Ensure sufficient disk for dumps/temp files/binlogs on source and destination.
- [ ] Set chunk size and concurrency conservatively for first run.
- [ ] Set lag and rate-control bounds.
- [ ] Validate retry/backoff and timeout policy.
- [ ] Schedule around long-running DDL windows.

Reference:

- https://docs.cloud.google.com/sql/docs/mysql/replication/replication-lag
- https://docs.aws.amazon.com/dms/latest/userguide/CHAP_Troubleshooting_Latency_Source_MySQL.html
- https://dev.mysql.com/doc/refman/8.4/en/metadata-locking.html

## I) Case sensitivity and naming portability

- [ ] Validate `lower_case_table_names` behavior on both sides.
- [ ] Confirm naming policy avoids mixed-case portability surprises.
- [ ] If upgrade touches `lower_case_table_names`, run uppercase-name checks first.

Reference:

- https://dev.mysql.com/doc/refman/8.4/en/identifier-case-sensitivity.html
- https://docs.oracle.com/cd/E17952_01/mysql-8.4-en/upgrade-prerequisites.html

## J) Runbook gates

- [ ] Dry-run plan reviewed and signed off.
- [ ] Baseline backup/snapshot confirmed.
- [ ] Logical backup rehearsal completed for the exact tool and workflow planned for rollback.
- [ ] Restore evidence reviewed: distinguish `backup completed`, `artifact validated`, and `restore usable`.
- [ ] If physical backup is part of rollback: prepare/apply-log procedure and exact tool-version compatibility confirmed separately.
- [ ] Rollback strategy documented.
- [ ] Post-run verification and report review assigned to owner.
