# Known Problems and Mitigations for MySQL/MariaDB Migration

Last reviewed: 2026-03-04

This document summarizes migration and replication failure modes observed in MySQL/MariaDB upgrades and cross-engine moves, and defines the default safeguards `dbmigrate` should implement.

## Source quality and scope

Primary sources are official MySQL and MariaDB documentation, with selected operator-facing incident writeups for practical failure patterns.

- MySQL 8.4 Reference Manual
- MariaDB Server Documentation and Release Notes
- Cloud provider troubleshooting docs (Google Cloud SQL, AWS DMS)

---

## 1) MariaDB <-> MySQL feature incompatibilities

### 1.1 JSON storage and semantics differ across engines

Evidence:

- MariaDB `JSON` is an alias of `LONGTEXT` and differs from MySQL native JSON storage and comparison behavior.
  - https://mariadb.com/docs/server/reference/data-types/string-data-types/json
- MariaDB documents that row-based replication of MySQL JSON to MariaDB does not work without conversion/workarounds.
  - https://mariadb.com/docs/server/reference/data-types/string-data-types/json
- MariaDB replication compatibility for MySQL 8.0 explicitly calls out JSON column limitations.
  - https://mariadb.com/docs/release-notes/community-server/about/compatibility-and-differences/replication-compatibility-between-mariadb-and-mysql

Observed failure patterns:

- Row events on JSON columns fail when source is MySQL JSON and target is MariaDB JSON alias/TEXT handling.
- Semantically different JSON comparisons produce verification diffs despite "same-looking" payloads.

`dbmigrate` safeguards:

- Preflight: detect JSON columns and engine pair; fail-fast in unsupported replication path.
- Plan warning: recommend explicit conversion (`JSON` -> `TEXT`/`LONGTEXT`) or statement-based migration route where appropriate.
- Verify mode: mark JSON comparison as potentially non-equivalent across engines unless normalization mode is enabled.

### 1.2 MariaDB-only features (sequences, temporal system versioning) are not symmetric with MySQL

Evidence:

- MariaDB has `CREATE SEQUENCE` and sequence objects.
  - https://mariadb.com/docs/server/reference/sql-structure/sequences/create-sequence
- MariaDB release notes list features available in MariaDB but not in MySQL, including temporal data tables and sequences.
  - https://mariadb.com/docs/release-notes/community-server/about/compatibility-and-differences/incompatibilities-and-feature-differences-between-mariadb-and-mysql-unmaint/incompatibilities-and-feature-differences-between-mariadb-11-0-and-mysql-8
- MariaDB system-versioned tables have replication/binlog caveats.
  - https://mariadb.com/docs/server/reference/sql-structure/temporal-tables/system-versioned-tables

Observed failure patterns:

- DDL apply failures in cross-engine moves when source uses engine-specific constructs.
- Replication re-apply issues with system-versioned tables due to implicit key behavior.

`dbmigrate` safeguards:

- Preflight scanner for unsupported DDL constructs (sequence objects, system-versioned clauses, engine-specific syntax).
- Default behavior: fail-fast with exact object list and remediation hints.
- `--apply-ddl` policy gate applied before replaying risky DDL in replication.

---

## 2) Replication incompatibilities and GTID boundaries

Evidence:

- MariaDB and MySQL GTID implementations are not compatible.
  - https://mariadb.com/docs/server/ha-and-performance/standard-replication/gtid
- MariaDB replication compatibility docs require file/position handling in mixed MariaDB/MySQL scenarios.
  - https://mariadb.com/docs/release-notes/community-server/about/compatibility-and-differences/replication-compatibility-between-mariadb-and-mysql
- Cross-version caveat: MySQL 8.0 -> MariaDB replication required specific MariaDB patch levels and MySQL binlog settings.
  - https://mariadb.com/docs/release-notes/community-server/about/compatibility-and-differences/replication-compatibility-between-mariadb-and-mysql

Observed failure patterns:

- Replication start-point mismatch and broken auto-position when operators assume GTID is portable across engines.
- Event incompatibility from newer MySQL binlog events when target MariaDB is below required patch level.

`dbmigrate` safeguards:

- Capability detection: engine/flavor/version + GTID mode at source and destination.
- Cross-engine default: checkpoint by file/position, not GTID.
- Plan output: explicit, versioned matrix warning with required settings (`binlog-row-value-options`, `binlog_transaction_compression`) when relevant.

---

## 3) Charset/collation pitfalls

Evidence:

- MySQL server defaults are `utf8mb4` + `utf8mb4_0900_ai_ci`.
  - https://dev.mysql.com/doc/refman/8.4/en/charset-server.html
- MySQL collation coercibility rules define "illegal mix" failure conditions.
  - https://dev.mysql.com/doc/refman/8.4/en/charset-collation-coercibility.html
- MariaDB documents charset/collation support differences against MySQL in compatibility pages.
  - https://mariadb.com/docs/release-notes/community-server/about/compatibility-and-differences/incompatibilities-and-feature-differences-between-mariadb-and-mysql-unmaint/incompatibilities-and-feature-differences-between-mariadb-11-0-and-mysql-8
- Example operator incidents with collation mismatch errors:
  - https://support.atlassian.com/crowd/kb/unable-to-perform-administrative-functions-in-crowd-console-due-to-error-illegal-mix-of-collations/

Observed failure patterns:

- "Illegal mix of collations" at runtime after upgrade or cross-engine migration.
- Replication failures when new/default collations are unsupported on the opposite engine/version.

`dbmigrate` safeguards:

- Preflight: full inventory of server/db/table/column charset+collation.
- Mapping stage: detect unsupported collations and propose deterministic replacements.
- Verification mode: explicit collation diff section, optional tolerance flags (`--tolerate-collation-diffs`) while still reporting mismatches.

---

## 4) Authentication plugin and account migration gotchas (MySQL 8+)

Evidence:

- MySQL 8.4 default auth plugin is `caching_sha2_password`.
  - https://dev.mysql.com/doc/refman/8.4/en/caching-sha2-pluggable-authentication.html
- MySQL 8.4 disables `mysql_native_password` server-side plugin by default.
  - https://dev.mysql.com/doc/refman/8.4/en/native-pluggable-authentication.html
  - https://dev.mysql.com/doc/relnotes/mysql/8.4/en/news-8-4-0.html
- MariaDB replication compatibility page notes that MySQL default auth may require separate replication user strategy in mixed setups.
  - https://mariadb.com/docs/release-notes/community-server/about/compatibility-and-differences/replication-compatibility-between-mariadb-and-mysql
- MariaDB authentication defaults differ (for example `unix_socket` behavior in MariaDB 10.4+ context).
  - https://mariadb.com/docs/server/security/user-account-management/authentication-from-mariadb-10-4

Observed failure patterns:

- Replication/user migration fails because plugin is unsupported or disabled on target.
- Client/tooling can no longer authenticate after MySQL 8.4 changes if plugin assumptions are stale.

`dbmigrate` safeguards:

- Account preflight: inventory accounts, auth plugins, TLS/RSA requirements, and plugin availability on destination.
- User/grant migration helper implemented with selectable scope (business-only or include system).
- Detailed compatibility report for each account with action class: migrate as-is, migrate with plugin rewrite, manual reset required, or blocked.

---

## 5) Reserved words and parser incompatibilities

Evidence:

- MySQL keyword/reserved-word list changes across versions and warns to review future reserved words before upgrades.
  - https://dev.mysql.com/doc/refman/8.0/en/keywords.html
- MySQL upgrade prerequisites explicitly warn that new reserved words can break existing identifiers.
  - https://docs.oracle.com/cd/E17952_01/mysql-8.4-en/upgrade-prerequisites.html
- MariaDB reserved words list differs and requires quoting when used as identifiers.
  - https://mariadb.com/docs/server/reference/sql-structure/sql-language-structure/reserved-words

Observed failure patterns:

- DDL replay failures due to unquoted identifiers that became reserved in destination version.
- Routine/view definitions break under parser differences.

`dbmigrate` safeguards:

- Plan-time parser scan over object names and definitions against source+destination reserved word sets.
- Auto-suggestion output for quote/rename remediation.
- Default behavior for unsafe parser conflicts: fail before migrate/replicate apply.

---

## 6) Binlog format tradeoffs and limitations

Evidence:

- MySQL documents row-based logging as default and recommends row-based to avoid nondeterministic statement replication issues.
  - https://dev.mysql.com/doc/refman/8.0/en/binary-log-formats.html
- MariaDB documents mixed as default and row-based as safest, with statement-based caveats.
  - https://mariadb.com/docs/server/server-management/server-monitoring-logs/binary-log/binary-log-formats
- MariaDB cross-engine replication guidance recommends `binlog_format=row`.
  - https://mariadb.com/docs/release-notes/community-server/about/compatibility-and-differences/replication-compatibility-between-mariadb-and-mysql

Observed failure patterns:

- Statement or mixed mode replays produce divergent results for nondeterministic workloads.
- Cross-engine replication breaks on unsupported event forms or statement semantics.

`dbmigrate` safeguards:

- Preflight enforcement: require/strongly recommend `ROW` for incremental mode.
- If not `ROW`: limited mode with high-visibility warning and explicit operator opt-in.
- Compatibility checks for compression and row-value options when MySQL->MariaDB paths require them.

---

## 7) Operational pain points (lag, large tx, long DDL, deadlocks, timeouts)

Evidence:

- Long-running transactions increase replication lag because binlog shipping waits for commit.
  - https://docs.cloud.google.com/sql/docs/mysql/replication/replication-lag
  - https://docs.aws.amazon.com/dms/latest/userguide/CHAP_Troubleshooting_Latency_Source_MySQL.html
- Metadata locks block DDL while transactions are open.
  - https://dev.mysql.com/doc/refman/8.4/en/metadata-locking.html

Observed failure patterns:

- Spiky apply lag after large commits.
- `ALTER TABLE` blocked for long periods due to metadata locks.
- Deadlock/retry storms and timeout cascades under high concurrency.

`dbmigrate` safeguards:

- Bounded batch/chunk apply with adaptive throttling.
- Backoff+retry policy for transient lock/deadlock errors.
- Throughput/lag metrics and progress reporting at table and stream level.
- Safety limits (`--max-events`, `--max-lag-seconds`, bounded worker queues).

### 7.1 Metadata-lock queue amplification during DDL windows

Evidence:

- MySQL metadata locking explains why DDL waits on active transactions and why object-level locks matter even without obvious row-lock pressure.
  - https://dev.mysql.com/doc/refman/8.4/en/metadata-locking.html
- MariaDB documents the same metadata-lock class and separate instrumentation options.
  - https://mariadb.com/kb/en/metadata-locking/
  - https://mariadb.com/docs/server/reference/plugins/other-plugins/metadata-lock-info-plugin

Observed failure patterns:

- `ALTER TABLE` or `RENAME TABLE` waits on an older transaction.
- Later ordinary reads/writes queue behind the waiting DDL, so the outage blast radius grows.
- Operators misclassify the incident as generic slowness or row-locking and retry the DDL instead of identifying the blocker.

`dbmigrate` safeguards:

- Replication apply classifies DDL lock-timeout failures with metadata-lock wording as `metadata_lock_timeout`, not only `retryable_transaction_error`.
- Operator rehearsal script: `scripts/run-metadata-lock-scenario.sh`.
- Runbook guidance prefers blocker identification and waiting-DDL abort decisions over blind retries.

### 7.2 Backup completion is not restore evidence

Evidence:

- MySQL recovery docs distinguish backup creation from actual recovery steps.
  - https://dev.mysql.com/doc/refman/8.4/en/recovery-from-backups.html
- MariaDB backup docs require explicit prepare and restore steps for physical tooling.
  - https://mariadb.com/kb/en/mariabackup-options/

Observed failure patterns:

- Teams claim rollback is safe because a backup job completed, but the restore path was never exercised.
- Dumps contain the expected bytes yet still fail operationally because stored objects, events, or restore assumptions were never smoke-tested.

`dbmigrate` safeguards:

- Operator rehearsal script: `scripts/run-backup-restore-rehearsal.sh`.
- Runbooks now distinguish backup completion, artifact validation, and restore usability.
- Physical backup tooling remains documented as a separate compatibility class rather than being conflated with logical migration success.

### 7.3 Session time zone changes make `TIMESTAMP` and `DATETIME` diverge operationally

Evidence:

- MySQL documents that time-zone handling affects temporal functions and `TIMESTAMP` behavior.
  - https://dev.mysql.com/doc/mysql-g11n-excerpt/8.0/en/time-zone-support.html
- MySQL replication docs call out time-zone-sensitive behavior explicitly.
  - https://dev.mysql.com/doc/refman/8.2/en/replication-features-timezone.html

Observed failure patterns:

- Teams compare rows across sessions or hosts and assume a shifted `TIMESTAMP` means corruption.
- Applications mix `TIMESTAMP` and `DATETIME` columns, then discover after cutover that local-time rendering and stored wall-clock values behave differently.

`dbmigrate` safeguards:

- Operator rehearsal script: `scripts/run-timezone-rehearsal.sh`.
- Runbooks now require explicit review of `system_time_zone`, session `time_zone`, and `TIMESTAMP` versus `DATETIME` semantics before compatibility claims.

---

## Additional high-impact checks to bake into preflight

- `lower_case_table_names` portability and immutability after initialization in MySQL 8+.
  - https://dev.mysql.com/doc/refman/8.4/en/identifier-case-sensitivity.html
- Upgrade-time lettercase mismatch risk when `lower_case_table_names=1`.
  - https://docs.oracle.com/cd/E17952_01/mysql-8.4-en/upgrade-prerequisites.html
- Trigger/definer upgrade hygiene checks called out in MySQL upgrade prerequisites.
  - https://docs.oracle.com/cd/E17952_01/mysql-8.4-en/upgrade-prerequisites.html

---

## Default policy implications for dbmigrate (derived from above)

1. Fail-fast on known incompatible features.
2. Prefer row-based incremental replication and file/position checkpoints on cross-engine GTID boundaries.
3. Treat charset/collation mapping as first-class planning output.
4. Keep user/grant migration optional but implemented, with detailed compatibility reporting.
5. Treat DDL replay as policy-gated (`--apply-ddl`), with conservative defaults.
6. Build operational controls in core path: chunking, backpressure, retries, lag-aware pacing, and transparent metrics.
