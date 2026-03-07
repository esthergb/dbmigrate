# Known Problems and Mitigations for MySQL/MariaDB Migration

Last reviewed: 2026-03-06

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

### 1.3 MySQL invisible columns, invisible indexes, and generated invisible primary keys drift across downgrade targets

Evidence:

- MySQL invisible columns and indexes are implemented through versioned DDL comments and metadata flags.
  - https://dev.mysql.com/doc/refman/8.4/en/invisible-columns.html
  - https://dev.mysql.com/doc/refman/8.4/en/invisible-indexes.html
- MySQL generated invisible primary keys can be included in logical dumps or removed with `--skip-generated-invisible-primary-key`.
  - https://dev.mysql.com/doc/refman/8.4/en/mysqldump.html
  - https://dev.mysql.com/doc/refman/9.6/en/create-table-gipks.html

Observed failure patterns:

- MySQL -> MariaDB restore accepts the DDL but materializes invisible columns and invisible indexes as visible objects.
- Included dumps preserve the GIPK column name on MariaDB, but it is no longer hidden.
- `--skip-generated-invisible-primary-key` removes the hidden PK from the logical schema entirely, which changes table shape even on MySQL targets that otherwise support GIPK.

`dbmigrate` safeguards:

- Current Phase 62 behavior: `plan` inventories invisible columns, invisible indexes, and GIPK tables in selected source databases.
- Schema `migrate` fails fast when the destination cannot preserve those hidden-schema semantics.
- Rehearsal support is provided by `scripts/run-invisible-gipk-rehearsal.sh` so operators can compare `dump included`, `dump skipped`, and restored DDL evidence before cutover.

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
- Current Phase 63 behavior:
  - `plan` inventories selected schema/table/column collations and persists `collation-precheck.json`.
  - Schema `migrate` fails fast when source collations are unsupported on the destination server.
  - `report` separates server-side incompatibility from client/library risk:
    - `unsupported_destination_count`
    - `client_compatibility_risk_count`
  - Focused rehearsal support is provided by `scripts/run-collation-rehearsal.sh`.
- Current local evidence:
  - `mysql84 -> mariadb10` with `utf8mb4_0900_ai_ci` failed both `plan` and logical restore with `ERROR 1273`.
  - `mariadb12 -> mysql84` with `utf8mb4_uca1400_ai_ci` failed both `plan` and logical restore with `ERROR 1273`.
  - `mariadb12 -> mariadb12` stayed schema-compatible but still emitted client-risk warnings for `utf8mb4_uca1400_ai_ci`.

### 3.1 Naive verify hashes produce false positives when representation differs

Evidence:

- MySQL and MariaDB can render the same logical row differently depending on connection charset, session `time_zone`, collation ordering, JSON formatting, and approximate numeric presentation.
- Phase 64 local rehearsal now demonstrates this directly with MySQL 8.4 -> MariaDB 12:
  - naive evidence hashes differed
  - canonicalized `hash`, `sample`, and `full-hash` all passed

Observed failure patterns:

- SQL-order-dependent checksum strategies drift when engines sort text differently.
- Session-level charset or timezone drift causes identical rows to hash differently.
- JSON objects with different key order look different in dumps or CLI output but are semantically equal.

`dbmigrate` safeguards:

- Current Phase 64 behavior:
  - verify hashing runs on pinned, normalized sessions (`SET NAMES utf8mb4`, `time_zone='+00:00'`)
  - hash/full-hash use stable-key ordered chunked streaming aggregation with bounded memory
  - hash/full-hash fail fast when a table has no primary key or non-null unique key
  - JSON payloads are canonicalized before hashing
  - verify artifacts record canonicalization assumptions and representation-sensitive table risk
  - `report` treats real verify diffs as `attention_required`, but keeps warning-only representation risk in `ok`
- Rehearsal support is provided by `scripts/run-verify-canonicalization-rehearsal.sh`.

### 3.2 Hot baseline copy can miss/duplicate rows with offset pagination

Evidence:

- `LIMIT/OFFSET` pagination is unstable under concurrent writes and deletes in live systems.
- Interrupted baselines that restart from table head can duplicate already copied rows.

Observed failure patterns:

- rows inserted/deleted during copy windows shift offsets and produce omissions/duplicates.
- restart logic that replays from table start can conflict with partially copied destination tables.

`dbmigrate` safeguards:

- Baseline uses source-side consistent snapshot reads (`REPEATABLE READ` + consistent snapshot transaction) for hot-copy stability.
- Baseline pagination is keyset-based on stable table keys (primary key or non-null unique key), not offset-based.
- Resume uses checkpoint cursor state (`key_columns`, typed `last_key_typed` with legacy `last_key` load compatibility) and destination max-key fallback when needed.
- Tables without stable key fail fast in live baseline mode as incompatible in `v1`.
- Baseline checkpoints capture source binlog watermark (`file:pos`) to preserve baseline->replicate continuity evidence.

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

- Current preflight: inventory visible source account plugins, destination plugin availability, and destination default-auth variable behavior.
- Current Phase 60 behavior: unsupported auth plugins are surfaced as warnings in `plan` output so account cutover work is explicit before any later user/grant step.
- Engine preflight runs in `plan` and schema `migrate`: unsupported source table engines fail before DDL apply.
- Detailed findings include per-account plugin mismatch reporting and per-table engine mismatch reporting.

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

### 7.4 Replication worker count does not rescue bad transaction shape

Evidence:

- MySQL and MariaDB both document that replication progress still depends on transaction dependency and commit boundaries, not worker count alone.
  - https://dev.mysql.com/doc/refman/8.4/en/replication-threads.html
  - https://dev.mysql.com/doc/mysql/8.0/en/replication-options-binary-log.html
  - https://mariadb.com/docs/server/ha-and-performance/standard-replication/parallel-replication

Observed failure patterns:

- One huge transaction dominates lag and checkpoint progress even when worker settings look generous.
- DDL, FK-heavy workloads, or keyless row matching collapse apparently parallel apply back into serialization.
- Operators keep tuning worker count while the real fix is smaller commits or better key coverage.

`dbmigrate` safeguards:

- Replication checkpoint artifacts now persist transaction-shape signals:
  - transactions seen/applied
  - max transaction event count
  - DDL/FK/keyless pressure
  - derived `risk_level` and `risk_signals`
- `report` now surfaces transaction-shape remediation proposals instead of treating worker count as the only tuning knob.
- Operator rehearsal script: `scripts/run-replication-shape-rehearsal.sh`.

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
