# Migration and Replication Conflict History

Last reviewed: 2026-03-06

This document is the long-lived research ledger for failure modes that can break MySQL/MariaDB baseline migration, downgrade, or incremental replication. It is intentionally broader than [known-problems.md](./known-problems.md): it mixes official incompatibility guidance with field incidents from Stack Overflow, DBA Stack Exchange, Reddit, and vendor KBs so we can keep extending the test matrix from real failures instead of from theory alone.

## Current index

- Latest major research pass:
  - Section `107-111` for metadata-lock runbooks, backup/restore validation gaps, time-zone and `NOW()` drift, plugin lifecycle and disabled-feature flags, and replication parallelism versus chunking.
- Current live research queue:
  - None. The active queue was closed after section `111`.
- Current PR-phase execution plan:
  - [matrix-pr-plan.md](./matrix-pr-plan.md), currently through Phase `61`.
- Current operator entry points:
  - [known-problems.md](./known-problems.md)
  - [risk-checklist.md](./risk-checklist.md)
- Current status note:
  - This document preserves historical queues and passes. As of `2026-03-06`, the high-priority active queue is closed and future additions should be justified by new field evidence or scope changes.

## How to maintain this file

- Add new findings as dated history entries rather than rewriting earlier notes away.
- Prefer official documentation for root cause and support boundaries.
- Add community links only when they show a concrete operational breakage worth simulating.
- When a fix is not reliable, say so explicitly.
- Treat every community-sourced issue as `UNCONFIRMED` until it has been reproduced locally or backed by a primary source.

## Evidence classes

- `official`: vendor reference manual, release note, or product documentation.
- `vendor`: cloud/vendor troubleshooting note or knowledge base.
- `community`: Stack Overflow, DBA Stack Exchange, Reddit, forum threads, blog posts.

## Update log

- `2026-03-06`: initial long-form catalog after PR #57. Scope expanded beyond current prechecks to include downgrade traps, client/protocol issues, partial-scope migration holes, and replication runtime failures seen in the field.
- `2026-03-06` second pass: extended source coverage, added current MySQL `9.x` notes, and expanded the catalog with dump-tool skew, charset alias deprecation, XA blockers, scheduled-event drift, and time-zone-table issues.
- `2026-03-06` third pass: added binlog-retention failure modes, replica identity cloning hazards, primary-key enforcement and GIPK divergence notes, plus stale-config startup failures after upgrades.
- `2026-03-06` fourth pass: corrected version scope to official `MySQL 9.6`, and added managed-service migration/failover constraints from AWS RDS/Aurora, Google Cloud SQL/DMS, and Azure Database for MySQL.
- `2026-03-06` fifth pass: focused on community incident threads for failover and replication recovery, adding DNS/connection-pool failover outages, skip-counter drift traps, and crash-recovery metadata failures.
- `2026-03-06` sixth pass: focused on schema-change incidents, online DDL tooling, and metadata-lock outages; backlog is now classified into locally simulable, cloud-only, and document-only items.
- `2026-03-06` seventh pass: added community incidents for auth/plugin breakage, collation/client-driver failures, and dump/import corruption; converted the locally simulable backlog into an ordered PR roadmap.
- `2026-03-06` eighth pass: added verification false positives, checksum/hash mismatch traps, and data-type comparison edge cases; resolved several prior open items and seeded the next research queue.
- `2026-03-06` ninth pass: created a separate PR-phase planning document and added seed findings for the next research queue topics.
- `2026-03-06` tenth pass: executed the next queued research topics for backup tooling, TLS/SSL transport failures, routine/view parser drift, large-object import limits, and multi-source filtering drift.
- `2026-03-06` eleventh pass: executed the refreshed queue topics for DDL `ALGORITHM/LOCK` drift, compression/page-format downgrade risk, parallel applier edge cases, connector/ORM metadata drift, and `LOAD DATA` privilege/path failures.
- `2026-03-06` twelfth pass: executed the next live queue for external-table edges, generated-column and expression-default drift, security-definer/default-role behavior, redo/undo capacity and long-transaction recovery, and client shell/gui divergence.
- `2026-03-06` thirteenth pass: executed the next live queue for optimizer statistics drift, GIS/FULLTEXT edge cases, event/time-zone replay, account-export and password-hash quirks, and proxy/router behavior.
- `2026-03-06` fourteenth pass: executed the next live queue for DDL copy/rebuild costs, charset handshake drift, trigger-order semantics, checksum-row canonicalization, and filtered-replication interactions with views and routines.
- `2026-03-06` fifteenth pass: executed the next live queue for optimizer-switch and SQL-mode behavior drift, replication recovery decision paths, foreign-key namespace quirks, purge/archive patterns, and observability metric drift.
- `2026-03-06` sixteenth pass: executed the next live queue for optimizer trace and hint drift, deeper GIS/SRS compatibility, event failure runbooks, account lock/expiry policy edges, and proxy read/write split behavior.
- `2026-03-06` seventeenth pass: executed the next live queue for GIPK runtime behavior, view/definer partial-scope interactions, chunking and autocommit semantics, monitoring agent/exporter compatibility, and non-InnoDB engine behavior.
- `2026-03-06` eighteenth pass: executed the next live queue for stored-object parser/sql-mode edges, temporary-table and session-state pitfalls, GTID reseed/surgery risk, filesystem case/path semantics, and replication-user/channel rotation.
- `2026-03-06` nineteenth pass: executed the final queued topics for metadata-lock observability runbooks, backup/restore validation gaps, session time-zone and `NOW()` drift, plugin lifecycle and disabled feature flags, and replication parallelism versus chunking; no new high-priority queue items were opened afterward.

## Version scope note

- As of `2026-03-06`, Oracle publishes official documentation and release notes for `MySQL 9.6.0` dated `2026-01-20`.
- This report therefore uses `MySQL 9.6` as the current `9.x` comparison point, while older `9.4` and `9.5` references remain useful for tracing earlier innovation-line behavior.
- Sources:
  - official: [MySQL 9.6 Release Notes](https://dev.mysql.com/doc/relnotes/mysql/9.6/en/)
  - official: [Changes in MySQL 9.6.0](https://dev.mysql.com/doc/relnotes/mysql/9.6/en/news-9-6-0.html)
  - official: [MySQL release notes index](https://dev.mysql.com/doc/relnotes/mysql/)

## 1. Unsupported version direction and downgrade assumptions

- Why it fails:
  - MySQL supports upgrade-oriented replication topologies and does not support replication from a newer source to an older replica.
  - MySQL downgrade support is restricted and can fail on dictionary, storage-format, or feature usage changes introduced by the newer server.
- Affected paths:
  - `MySQL 8.4 -> MySQL 8.0`
  - `MySQL 9.6 -> MySQL 8.4` or `MySQL 8.0`
  - any same-engine downgrade that assumes "same family" means "safe"
- How to simulate:
  - Create objects using features or defaults introduced in the newer line.
  - Attempt logical import or replication into the older target.
  - Include `CHECK TABLE ... FOR UPGRADE` and downgrade dry runs in the scenario.
- How to detect:
  - Capture exact source and target versions before planning.
  - Run server-native upgrade/downgrade checks where available.
  - Flag any path where source version is newer than replica version.
- Mitigation / fix:
  - Treat downgrade as an explicit compatibility matrix problem, not a generic same-engine path.
  - Prefer logical export/import and explicit prechecks over in-place downgrade assumptions.
  - Fail by default when the version pair is outside the approved matrix.
- dbmigrate implication:
  - Keep downgrade profile enforcement hard-fail by default and keep version ranges explicit.
- Sources:
  - official: [MySQL downgrading](https://dev.mysql.com/doc/refman/en/downgrading.html)
  - official: [MySQL replication topology upgrade/downgrade rules](https://dev.mysql.com/doc/en/replication-upgrade.html)
  - official: [MySQL upgrade best practices](https://dev.mysql.com/doc/refman/8.4/en/upgrade-best-practices.html)

## 2. Cross-engine GTID mismatch and wrong checkpoint model

- Why it fails:
  - MariaDB GTID and MySQL GTID are different implementations and are not portable across engines.
  - Operators often assume `auto-position` can survive cross-engine replication when it cannot.
- Affected paths:
  - `MySQL -> MariaDB`
  - `MariaDB -> MySQL`
- How to simulate:
  - Enable GTID on the source.
  - Attempt to start cross-engine replication using GTID auto-position semantics instead of file/position.
- How to detect:
  - Record engine flavor and GTID mode on both sides.
  - Fail planning if the requested start point is GTID for a cross-engine path.
- Mitigation / fix:
  - Use binlog file and position for cross-engine checkpoints.
  - Convert GTID expectations into an operator warning plus explicit start-point instructions.
- dbmigrate implication:
  - Keep cross-engine checkpointing on file/position only.
- Sources:
  - official: [Replication compatibility between MariaDB and MySQL](https://mariadb.com/docs/release-notes/community-server/about/compatibility-and-differences/replication-compatibility-between-mariadb-and-mysql)
  - official: [MariaDB replication setup notes](https://mariadb.com/docs/server/ha-and-performance/standard-replication/setting-up-replication)

## 3. Binlog option incompatibilities: compression, encryption, row image, event support

- Why it fails:
  - Cross-engine replication is sensitive to binlog options such as compression, encryption, and event encoding details.
  - Row events can become unreadable or unsupported if the source uses features the target parser does not understand.
- Affected paths:
  - mostly `MySQL -> MariaDB` and `MariaDB -> MySQL`
- How to simulate:
  - Run replication with compressed or encrypted binlogs on MariaDB.
  - Vary `binlog_row_image` and newer event behaviors on MySQL.
  - Compare apply behavior with full versus reduced row images.
- How to detect:
  - Inspect source replication settings before baseline or incremental runs.
  - Reject unsupported settings instead of discovering them mid-stream.
- Mitigation / fix:
  - Standardize on `ROW` format and full row images for incremental apply.
  - Disable MariaDB binlog compression and encryption when targeting MySQL.
  - Add explicit report output for the exact source settings that make the path unsafe.
- dbmigrate implication:
  - Binlog capability preflight must stay strict and version-aware.
- Sources:
  - official: [Replication compatibility between MariaDB and MySQL](https://mariadb.com/docs/release-notes/community-server/about/compatibility-and-differences/replication-compatibility-between-mariadb-and-mysql)
  - community: [MySQL/MariaDB replication JSON field type breakage on row events](https://www.reddit.com/r/mariadb/comments/14i1krq)

## 4. Replication breakage after schema drift or out-of-order DDL

- Why it fails:
  - Replication assumes source and target table definitions remain compatible at row-apply time.
  - Online `ALTER TABLE`, delayed DDL propagation, or foreign-key creation in the wrong order can make row events no longer match target metadata.
- Affected paths:
  - same-engine replication
  - cross-engine replication
  - baseline schema replay followed by incremental apply
- How to simulate:
  - Start replication.
  - Apply DDL on the source while writes continue.
  - Delay or reorder DDL on the target, especially around child tables and foreign keys.
- How to detect:
  - Compare source and target `SHOW CREATE TABLE` before starting apply.
  - Detect DDL touching replicated tables and block risky cases unless policy allows them.
  - Monitor for apply errors that mention table definition mismatch or FK creation failure.
- Mitigation / fix:
  - Quiesce writes during risky schema changes.
  - Apply dependent DDL in topological FK order.
  - Fail fast on risky DDL by default and require explicit operator override.
- dbmigrate implication:
  - FK-order planning and DDL conflict reporting remain first-class.
- Sources:
  - official: [Metadata locking](https://dev.mysql.com/doc/en/metadata-locking.html)
  - community: [Replication failed after schema changes on master](https://stackoverflow.com/questions/45057803/mysql-replication-failed-after-changes-on-master-scheme)
  - community: [Table definition mismatch field report](https://stackoverflow.com/questions/46344943/mysql-replication-does-not-work-for-all-tables)

## 5. Native JSON versus MariaDB JSON alias and row-event incompatibility

- Why it fails:
  - MySQL stores JSON in a native binary format.
  - MariaDB treats `JSON` as an alias of `LONGTEXT` and does not accept all MySQL JSON replication behaviors.
  - JSON comparison semantics also differ, causing verify noise even when text payloads look similar.
- Affected paths:
  - `MySQL -> MariaDB` baseline and replication
  - cross-engine verification
- How to simulate:
  - Create MySQL tables with native `JSON` columns and row-based replication enabled.
  - Insert nested JSON documents and replicate to MariaDB.
- How to detect:
  - Inventory JSON columns during preflight.
  - Flag row-based `MySQL JSON -> MariaDB` as incompatible unless a conversion strategy is selected.
- Mitigation / fix:
  - Convert JSON columns to `TEXT` or `LONGTEXT` before moving to MariaDB.
  - Prefer logical export/import or statement-based workarounds only when the risk is fully understood.
  - Normalize JSON on verification if semantic rather than byte equality is needed.
- dbmigrate implication:
  - Keep failing by default for unsupported JSON replication paths.
- Sources:
  - official: [MariaDB 11.0 vs MySQL 8 incompatibilities](https://mariadb.com/docs/release-notes/community-server/about/compatibility-and-differences/incompatibilities-and-feature-differences-between-mariadb-and-mysql-unmaint/incompatibilities-and-feature-differences-between-mariadb-11-0-and-mysql-8)
  - official: [Replication compatibility between MariaDB and MySQL](https://mariadb.com/docs/release-notes/community-server/about/compatibility-and-differences/replication-compatibility-between-mariadb-and-mysql)
  - community: [Row-based replication receives unknown JSON field type](https://www.reddit.com/r/mariadb/comments/14i1krq)

## 6. Generated and virtual column syntax differences in dumps

- Why it fails:
  - MySQL and MariaDB do not serialize generated columns identically.
  - Dumps produced by one server can include syntax or generated-value behavior the other server rejects.
- Affected paths:
  - `MySQL -> MariaDB`
  - `MariaDB -> MySQL`
- How to simulate:
  - Create virtual and stored generated columns.
  - Export with the source server's dump tool.
  - Import into the other engine without transformation.
- How to detect:
  - Scan DDL for generated columns before import.
  - Flag cross-engine dump replay when generated-column syntax differs from target support.
- Mitigation / fix:
  - Rewrite generated column DDL for the target engine before apply.
  - Prefer tool-driven extraction over raw dump replay when generated columns are present.
- dbmigrate implication:
  - Generated-column normalization should stay in the transformation backlog.
- Sources:
  - community: [Importing virtual columns from MySQL to MariaDB fails](https://stackoverflow.com/questions/68008727/error-when-importing-virtual-column-from-mysql-to-mariadb)
  - community: [Restoring dumps that contain generated columns](https://stackoverflow.com/questions/46127855/how-to-restore-mysql-backup-that-have-generated-always-as-column)

## 7. Invisible columns, invisible indexes, and generated invisible primary key surprises

- Why it fails:
  - Newer MySQL lines support invisible columns and invisible index behavior that older targets or different engines may not interpret the same way.
  - These features can also affect dump output, application expectations, and downgrade safety.
- Affected paths:
  - `MySQL 8.4 -> MySQL 8.0`
  - `MySQL -> MariaDB`
- How to simulate:
  - Create tables with invisible columns and indexes.
  - Add tables without explicit primary keys and test newer automatic key behavior separately from legacy assumptions.
- How to detect:
  - Scan `INFORMATION_SCHEMA.COLUMNS` and DDL text for invisibility attributes.
  - Reject downgrade plans that include unsupported visibility semantics.
- Mitigation / fix:
  - Materialize invisible columns as normal columns before downgrade or cross-engine move.
  - Rebuild indexes explicitly if the target lacks equivalent behavior.
- dbmigrate implication:
  - Add visibility-feature probes to the compatibility suite before enabling broader downgrade pairs.
- Sources:
  - official: [MySQL invisible columns](https://dev.mysql.com/doc/refman/8.4/en/invisible-columns.html)
  - official: [MySQL invisible indexes](https://dev.mysql.com/doc/refman/8.4/en/invisible-indexes.html)

## 8. Charset and collation drift, including `utf8mb4_0900_ai_ci` and `uca1400`

- Why it fails:
  - MySQL 8 commonly defaults to `utf8mb4_0900_ai_ci`.
  - Newer MariaDB lines introduce `uca1400` collations that many MySQL clients and some connectors do not understand.
  - Cross-engine replication and restore break when the destination or client does not support the collation name.
- Affected paths:
  - `MySQL -> MariaDB`
  - `MariaDB -> MySQL`
  - client cutovers after engine switch
- How to simulate:
  - Create schemas and tables using `utf8mb4_0900_ai_ci` on MySQL.
  - Create schemas using `utf8mb4_uca1400_ai_ci` on MariaDB.
  - Test dump import and client connection with older drivers.
- How to detect:
  - Inventory server, database, table, and column collations.
  - Check client libraries used by the application, not only server support.
  - Fail preflight when an object collation is unsupported on the target.
- Mitigation / fix:
  - Map unsupported collations to approved equivalents before apply.
  - Distinguish storage collation from connection collation if client libraries lag behind server features.
  - Report the mapping explicitly because collation changes can alter sort or comparison behavior.
- dbmigrate implication:
  - Keep collation inventory in reports and add client-compatibility notes where possible.
- Sources:
  - official: [MariaDB 11.0 vs MySQL 8 incompatibilities](https://mariadb.com/docs/release-notes/community-server/about/compatibility-and-differences/incompatibilities-and-feature-differences-between-mariadb-and-mysql-unmaint/incompatibilities-and-feature-differences-between-mariadb-11-0-and-mysql-8)
  - community: [Unknown collation `utf8mb4_0900_ai_ci`](https://stackoverflow.com/questions/62829879/error-1273-but-my-schema-dont-contain-utf8mb4-0900-ai-ci)
  - community: [Rails migration emits `utf8mb4_0900_ai_ci` that older servers reject](https://stackoverflow.com/questions/58930916/how-to-create-migrations-with-specific-collate)
  - community: [MariaDB `uca1400` collation breaks PHP clients](https://www.reddit.com/r/PHPhelp/comments/1av88ip)
  - community: [Same `uca1400` issue discussed in r/Database](https://www.reddit.com/r/Database/comments/1av8ch1)

## 9. `lower_case_table_names` and filesystem portability

- Why it fails:
  - Table-name case behavior depends on `lower_case_table_names` and the underlying filesystem.
  - The setting is initialization-sensitive and not something you safely flip later.
  - Moving between environments can silently collapse object names or create restore surprises.
- Affected paths:
  - platform migration
  - MySQL to MariaDB cutover on a host initialized with different case rules
- How to simulate:
  - Create mixed-case table names.
  - Initialize source and target with different `lower_case_table_names` values.
  - Perform dump and restore.
- How to detect:
  - Record `lower_case_table_names` for both sides during preflight.
  - Scan for schemas containing names that differ only by case.
- Mitigation / fix:
  - Enforce matching case rules before migration.
  - Fail when the schema contains case collisions that the target cannot preserve.
- dbmigrate implication:
  - This should remain a hard precheck because auto-fix is dangerous.
- Sources:
  - official: [Identifier case sensitivity](https://dev.mysql.com/doc/refman/8.4/en/identifier-case-sensitivity.html)
  - official: [Preparing your installation for upgrade](https://docs.oracle.com/cd/E17952_01/mysql-8.4-en/upgrade-prerequisites.html)
  - community: [MariaDB installation and `lower_case_table_names` migration issue](https://dba.stackexchange.com/questions/346991/mariadb-installation-setting-lower-case-table-names)

## 10. Temporal defaults, zero dates, invalid dates, and SQL mode drift

- Why it fails:
  - Older datasets often store zero dates or partially zero dates.
  - MySQL 8 strict defaults reject those values where MariaDB may still accept them, depending on `sql_mode`.
  - This shows up as `ERROR 1067 Invalid default value` during DDL replay.
- Affected paths:
  - legacy baseline migration into strict MySQL
  - dump replay after engine change
  - downgrade if defaults were normalized on one side but not the other
- How to simulate:
  - Create `DATE`, `DATETIME`, and `TIMESTAMP` columns with zero-date defaults and invalid calendar defaults.
  - Reapply DDL into strict-mode MySQL 8.x.
- How to detect:
  - Inspect temporal defaults in `INFORMATION_SCHEMA.COLUMNS`.
  - Record destination `sql_mode`.
  - Fail plan and migrate before any DDL is applied.
- Mitigation / fix:
  - Rewrite zero-date defaults to `NULL` or a valid sentinel value.
  - Emit exact `ALTER TABLE ... SET DEFAULT ...` proposals.
- dbmigrate implication:
  - Already partly implemented; keep extending the precheck and dataset coverage.
- Sources:
  - official: [MySQL SQL mode](https://dev.mysql.com/doc/refman/8.4/en/sql-mode.html)
  - official: [MySQL date and time data types](https://dev.mysql.com/doc/refman/8.4/en/date-and-time-types.html)
  - official: [MariaDB SQL mode](https://mariadb.com/kb/en/sql-mode/)
  - community: [Upgrade check warning for zero date defaults on MySQL 8](https://stackoverflow.com/questions/77553746/column-has-zero-default-value-0000-00-00-000000)

## 11. `DEFINER`, `SQL SECURITY`, and orphan stored objects

- Why it fails:
  - Views, routines, triggers, and events can carry a `DEFINER` that does not exist on the target.
  - Triggers and events always execute in definer context, which makes missing or over-privileged definers especially dangerous.
- Affected paths:
  - logical dump/import
  - partial database migration
  - account migration disabled or incomplete
- How to simulate:
  - Create views, routines, and triggers using a dedicated application definer.
  - Exclude that account from migration and then restore the objects.
- How to detect:
  - Compare all object definers against destination accounts.
  - Flag `SQL SECURITY DEFINER` objects separately from `INVOKER`.
  - Detect orphan objects before runtime.
- Mitigation / fix:
  - Recreate the missing account, or rewrite the objects to an approved definer.
  - Prefer `SQL SECURITY INVOKER` where semantics allow it.
- dbmigrate implication:
  - Precheck should report missing definers even when user/grant migration is disabled.
- Sources:
  - official: [Stored object access control](https://dev.mysql.com/doc/refman/en/stored-objects-security.html)
  - community: [MySQL error 1449 definer does not exist](https://stackoverflow.com/questions/35773454/using-triggers-with-different-users-mysql-error-1449)
  - community: [Imported database fails because definer user is missing](https://stackoverflow.com/questions/36223367/the-user-specified-as-a-definer-does-not-exist)

## 12. Authentication plugin mismatch and account model divergence

- Why it fails:
  - MySQL 8.4 and the current `9.4` line use the newer authentication model around `caching_sha2_password`, while `mysql_native_password` is deprecated or absent from the default modern path.
  - MariaDB 10.4+ changed account storage and uses socket authentication by default for key local accounts.
  - Password hashes and plugin semantics cannot always be copied as-is across engines.
- Affected paths:
  - user/grant migration
  - application cutover
  - cross-engine replication users
  - `MySQL 9.6 -> older MySQL clients or mixed MySQL/MariaDB estates`
- How to simulate:
  - Create accounts using `caching_sha2_password`, `mysql_native_password`, and MariaDB socket-based defaults.
  - Attempt login with clients that only support older auth methods.
  - Attempt cross-engine grant migration without plugin rewrite.
- How to detect:
  - Inventory every source account's authentication plugin.
  - Check destination plugin availability and startup defaults.
  - Flag accounts requiring TLS or RSA support.
- Mitigation / fix:
  - Rewrite incompatible accounts to a mutually supported plugin or force password reset on cutover.
  - Distinguish business accounts from system accounts in reports.
  - Make client-driver capability part of the migration plan, not a post-cutover surprise.
- dbmigrate implication:
  - The detailed user/grant compatibility report is mandatory, not optional polish.
- Sources:
  - official: [Caching SHA-2 authentication](https://dev.mysql.com/doc/refman/en/caching-sha2-pluggable-authentication.html)
  - official: [Native pluggable authentication](https://dev.mysql.com/doc/refman/8.4/en/native-pluggable-authentication.html)
  - official: [Authentication from MariaDB 10.4](https://mariadb.com/docs/server/security/user-account-management/authentication-from-mariadb-10-4)
  - community: [Client does not support `caching_sha2_password`](https://stackoverflow.com/questions/51670095/docker-flyway-mysql-8-client-does-not-support-authentication-protocol-requested)
  - community: [Password hashing differences discussion](https://stackoverflow.com/questions/49300674/differences-in-password-hashing-between-mysql-and-mariadb)

## 13. Roles, grants, and system-schema differences

- Why it fails:
  - MariaDB and MySQL diverge in role behavior, privilege metadata, and internal account storage.
  - Partial account migration can leave routines or application flows referencing roles or grants that do not exist on the target.
- Affected paths:
  - user/grant migration
  - same-database cutovers that expect privilege parity
- How to simulate:
  - Create users with inherited roles and role activation requirements.
  - Export/import grants between engines and compare effective permissions.
- How to detect:
  - Inventory grants, roles, role membership, and account plugin data as separate dimensions.
  - Detect references to system schemas and system users.
- Mitigation / fix:
  - Rebuild grants from normalized intent rather than copying raw system-table rows across engines.
  - Report effective privilege drift, not only syntactic drift.
- dbmigrate implication:
  - User/grant migration should remain report-first and never silently coerce system metadata.
- Sources:
  - official: [MariaDB 11.0 vs MySQL 8 incompatibilities](https://mariadb.com/docs/release-notes/community-server/about/compatibility-and-differences/incompatibilities-and-feature-differences-between-mariadb-and-mysql-unmaint/incompatibilities-and-feature-differences-between-mariadb-11-0-and-mysql-8)
  - official: [Authentication from MariaDB 10.4](https://mariadb.com/docs/server/security/user-account-management/authentication-from-mariadb-10-4)

## 14. Engine-specific objects, storage engines, partition handlers, and tablespaces

- Why it fails:
  - MariaDB and MySQL each support features the other lacks: sequences, system-versioned tables, alternative storage engines, and partitioning differences.
  - Even when DDL parses, storage behavior may not be equivalent.
- Affected paths:
  - cross-engine baseline migration
  - downgrade into older MariaDB or MySQL lines
- How to simulate:
  - Create tables using MariaDB-only features such as sequences or system versioning.
  - Create partitioned tables and test import or replication into the other engine.
  - Include explicit tablespace references in DDL where supported.
- How to detect:
  - Parse DDL for unsupported clauses and engine names.
  - Inventory partitioning, tablespace use, and storage-engine selection before migration.
- Mitigation / fix:
  - Fail fast with object-by-object findings.
  - Offer logical rewrite paths only where semantics can be preserved.
  - Prefer manual remediation for system-versioned and sequence-backed objects.
- dbmigrate implication:
  - Unsupported-engine object discovery should remain a hard gate.
- Sources:
  - official: [MariaDB 11.0 vs MySQL 8 incompatibilities](https://mariadb.com/docs/release-notes/community-server/about/compatibility-and-differences/incompatibilities-and-feature-differences-between-mariadb-and-mysql-unmaint/incompatibilities-and-feature-differences-between-mariadb-11-0-and-mysql-8)
  - official: [MariaDB vs MySQL compatibility overview](https://mariadb.com/docs/release-notes/compatibility-and-differences/mariadb-vs-mysql-compatibility)
  - community: [Partitioned-table replication copy failed until logical dump was used](https://stackoverflow.com/questions/30613363/mysql-master-slave-partitioned-table-doesnt-exists)

## 15. Foreign-key and constraint behavior changes

- Why it fails:
  - Foreign-key enforcement, naming, and validation rules differ by version and engine.
  - MySQL 8.4 tightened behavior around non-standard foreign-key references.
  - Constraint DDL often fails when replayed out of dependency order.
- Affected paths:
  - same-engine downgrade
  - `MySQL -> MariaDB`
  - baseline schema apply with partial object ordering
- How to simulate:
  - Create child tables before parents.
  - Use foreign keys that reference keys accepted by older versions but flagged by newer behavior.
  - Restore the schema in shuffled order.
- How to detect:
  - Build a dependency graph for FK creation order.
  - Probe for non-standard FK usage during planning.
- Mitigation / fix:
  - Apply tables first, then constraints in topological order.
  - Reject non-standard FK definitions when the destination tightens validation.
- dbmigrate implication:
  - The known FK-order limitation was real; keep hardening around it.
- Sources:
  - official: [MySQL foreign key constraints](https://dev.mysql.com/doc/refman/8.4/en/create-table-foreign-keys.html)
  - official: [MySQL upgrade checker utility checks foreign-key issues](https://dev.mysql.com/doc/mysql-shell/8.4/en/mysql-shell-utilities-upgrade.html)

## 16. Reserved words, parser drift, and routine syntax changes

- Why it fails:
  - Keywords and parser behavior change over time.
  - Object names or routine bodies that were valid on the source may fail on the target unless quoted or rewritten.
- Affected paths:
  - same-engine upgrades and downgrades
  - cross-engine imports of procedures, views, and triggers
- How to simulate:
  - Create objects using identifiers that became reserved later.
  - Restore the schema into the newer target.
- How to detect:
  - Compare object names and routine tokens against source and destination reserved-word sets.
  - Run server-native upgrade checking where available.
- Mitigation / fix:
  - Quote or rename conflicting identifiers before migration.
  - Report parser-risk objects separately from data-only issues.
- dbmigrate implication:
  - This belongs in preflight, not in runtime apply errors.
- Sources:
  - official: [Preparing your installation for upgrade](https://docs.oracle.com/cd/E17952_01/mysql-8.4-en/upgrade-prerequisites.html)
  - official: [MySQL Shell upgrade checker utility](https://dev.mysql.com/doc/mysql-shell/8.4/en/mysql-shell-utilities-upgrade.html)

## 17. Partial database migrations leave hidden dependencies behind

- Why it fails:
  - Migrating only selected databases can omit users, events, routines, views, or cross-database references that the included objects depend on.
  - The migration appears successful until runtime hits the missing dependency.
- Affected paths:
  - any partial-scope baseline migration
  - any partial-scope verification or cutover rehearsal
- How to simulate:
  - Create a view or routine in database `A` that references database `B`.
  - Migrate only `A`.
  - Exclude a required definer account or event scheduler dependency.
- How to detect:
  - Analyze object definitions for cross-database references.
  - Compare included schemas against referenced schemas and required accounts.
  - Emit warnings for events and routines excluded by scope.
- Mitigation / fix:
  - Support partial databases, but report every omitted dependency explicitly.
  - Require operator acknowledgement when the selected scope is not self-contained.
- dbmigrate implication:
  - Partial database support is fine only if dependency reporting is strict.
- Sources:
  - official: [Stored object access control](https://dev.mysql.com/doc/refman/en/stored-objects-security.html)
  - community: [Definer-related import failures often surface only after import seems successful](https://stackoverflow.com/questions/35773454/using-triggers-with-different-users-mysql-error-1449)

## 18. Metadata locks, long transactions, and DDL blocking

- Why it fails:
  - Long-running transactions hold metadata locks.
  - DDL waits behind those locks, replication lag grows, and maintenance windows turn into stalled cutovers.
- Affected paths:
  - baseline migration during live writes
  - replication apply
  - schema verification and schema drift correction
- How to simulate:
  - Start a transaction that reads or writes a target table and keep it open.
  - In another session, run `ALTER TABLE` or other DDL.
  - Observe lag and blocking behavior.
- How to detect:
  - Query metadata lock instrumentation before and during migration windows.
  - Track lag and waiting sessions in reports.
- Mitigation / fix:
  - Quiesce write-heavy workloads for risky DDL.
  - Break data copy into bounded chunks.
  - Use backoff and clear abort conditions instead of waiting forever.
- dbmigrate implication:
  - Runtime safety limits remain necessary even when compatibility checks are green.
- Sources:
  - official: [Metadata locking](https://dev.mysql.com/doc/en/metadata-locking.html)
  - vendor: [Cloud SQL replication lag guidance](https://docs.cloud.google.com/sql/docs/mysql/replication/replication-lag)
  - vendor: [AWS DMS MySQL latency troubleshooting](https://docs.aws.amazon.com/dms/latest/userguide/CHAP_Troubleshooting_Latency_Source_MySQL.html)

## 19. Spatial index and physical-format upgrade/downgrade hazards

- Why it fails:
  - Some release-specific changes introduce storage or index hazards that are not obvious from logical schema alone.
  - MySQL 8.4.4+ specifically warns about spatial indexes during upgrade and notes downgrade reintroduces the underlying issue.
- Affected paths:
  - same-engine MySQL upgrade/downgrade
- How to simulate:
  - Create tables with spatial indexes.
  - Perform staged upgrade/downgrade rehearsal with writes against those tables.
- How to detect:
  - Inventory spatial indexes before upgrade.
  - Flag affected versions in the plan.
- Mitigation / fix:
  - Drop and recreate spatial indexes around the upgrade window as recommended by MySQL.
- dbmigrate implication:
  - Version-specific object checks need room for surgical exceptions, not only generic engine rules.
- Sources:
  - official: [Changes in MySQL 8.4](https://dev.mysql.com/doc/refman/en/upgrading-from-previous-series.html)
  - official: [MySQL 8.4.4 release notes](https://dev.mysql.com/doc/relnotes/mysql/8.4/en/news-8-4-4.html)

## 20. Client and protocol incompatibilities after the server move

- Why it fails:
  - A migration may finish at the server layer but still fail at the client layer because drivers, protocol expectations, or authentication support lag behind the new server.
  - New collations and auth plugins are common triggers.
- Affected paths:
  - application cutover after baseline migration
  - same-engine upgrades with old connectors
  - MariaDB client to MySQL 8.4 or vice versa
- How to simulate:
  - Run representative application clients against both source and target after migration.
  - Include old client libraries in a smoke suite, not only the newest connector.
- How to detect:
  - Record connector versions as part of the operator checklist.
  - Add connection smoke tests to the migration rehearsal, not just server-side validation.
- Mitigation / fix:
  - Upgrade drivers before or with the database cutover.
  - If needed, temporarily choose mutually supported auth plugins or connection collations.
- dbmigrate implication:
  - The report should call out client-side risk even when dbmigrate itself can connect successfully.
- Sources:
  - official: [Caching SHA-2 authentication](https://dev.mysql.com/doc/refman/en/caching-sha2-pluggable-authentication.html)
  - official: [MariaDB protocol differences with MySQL](https://mariadb.com/docs/server/reference/clientserver-protocol/mariadb-protocol-differences-with-mysql)
  - community: [Flyway / MariaDB client cannot authenticate against MySQL 8](https://stackoverflow.com/questions/51670095/docker-flyway-mysql-8-client-does-not-support-authentication-protocol-requested)
  - community: [MariaDB `uca1400` collation breaks older PHP clients](https://www.reddit.com/r/PHPhelp/comments/1av88ip)

## 21. Replication filters and default-database surprises

- Why it fails:
  - Statement-based filtering can behave unexpectedly when statements do not use the default database the operator thinks they do.
  - This creates "replication looks healthy but some objects never moved" scenarios.
- Affected paths:
  - filtered replication
  - partial incremental sync
- How to simulate:
  - Configure replication filters for a specific schema.
  - Execute statements without `USE dbname` or with cross-database references.
- How to detect:
  - Inspect replication filter configuration and replay mode.
  - Compare source writes against replicated object counts, not just thread status.
- Mitigation / fix:
  - Avoid statement-based filtering for correctness-critical migrations.
  - Prefer explicit include/exclude semantics in extraction and apply logic.
- dbmigrate implication:
  - Report should distinguish "healthy stream" from "complete scope coverage".
- Sources:
  - community: [Replication changes not being sent due to default database behavior](https://stackoverflow.com/questions/5174327/mysql-replication-changes-not-being-sent)

## 22. Dump-tool and import/export option skew

- Why it fails:
  - Logical migration is not just about server versions; dump-tool defaults can inject statements or metadata the destination cannot parse.
  - Common breakpoints are `COLUMN_STATISTICS`, `SET @@GLOBAL.GTID_PURGED`, and version-specific session statements emitted by newer MySQL tooling.
- Affected paths:
  - `MySQL 8.4/9.6 dump tool -> MariaDB`
  - `MySQL 8.4/9.6 dump tool -> older MySQL`
  - operator fallback paths that bypass dbmigrate and use `mysqldump`
- How to simulate:
  - Generate dumps from MySQL 8.4 and 9.6 with default options.
  - Re-import into MariaDB and older MySQL lines.
  - Repeat with `--column-statistics=0` and `--set-gtid-purged=OFF`.
- How to detect:
  - Record the exact dump client version, not only the server version.
  - Scan dumps for `COLUMN_STATISTICS`, `GTID_PURGED`, and versioned comments before import.
- Mitigation / fix:
  - Standardize dump options for cross-version or cross-engine moves.
  - Prefer dbmigrate-native extraction paths over raw dump replay when version skew is large.
- dbmigrate implication:
  - Operator docs should keep warning that external dump tools can reintroduce compatibility failures outside dbmigrate's controlled path.
- Sources:
  - official: [mysqldump reference](https://dev.mysql.com/doc/refman/8.4/en/mysqldump.html)
  - official: [MySQL Shell upgrade checker utility](https://dev.mysql.com/doc/mysql-shell/8.4/en/mysql-shell-utilities-upgrade.html)
  - community: [mysqldump unknown table `COLUMN_STATISTICS`](https://stackoverflow.com/questions/52423595/mysqldump-couldnt-execute-unknown-table-column-statistics-in-information-sc)
  - community: [mysqldump version mismatch caused by wrong client binary](https://stackoverflow.com/questions/73829859/mysqldump-version-mismatch-but-i-have-the-newest-of-both)

## 23. `utf8mb3` deprecation and charset alias drift

- Why it fails:
  - Older estates often still use `utf8`, which in MySQL historically maps to `utf8mb3`.
  - Modern MySQL lines warn about or deprecate that alias, while MariaDB and client stacks can preserve or interpret the old naming differently.
- Affected paths:
  - same-engine upgrade into stricter MySQL lines
  - cross-engine migrations that normalize to `utf8mb4`
- How to simulate:
  - Create schemas and columns using explicit `utf8` or `utf8mb3`.
  - Recreate them on newer MySQL and MariaDB lines with strict upgrade checks enabled.
- How to detect:
  - Inventory all character sets and look specifically for `utf8` alias use.
  - Treat alias-based definitions separately from explicit `utf8mb4`.
- Mitigation / fix:
  - Normalize `utf8` and `utf8mb3` definitions to an approved `utf8mb4` policy before migration.
  - Report the impact because index length, sorting, and client expectations can change.
- dbmigrate implication:
  - Charset inventory should highlight alias use, not only unsupported collations.
- Sources:
  - official: [The utf8 character set alias and utf8mb3](https://dev.mysql.com/doc/refman/8.4/en/charset-unicode-utf8.html)
  - official: [MySQL Shell upgrade checker utility](https://dev.mysql.com/doc/mysql-shell/8.4/en/mysql-shell-utilities-upgrade.html)

## 24. Prepared XA transactions block upgrade and complicate cutover

- Why it fails:
  - In-doubt or prepared XA transactions survive longer than operators expect and can block upgrade or cutover operations.
  - These states are easy to miss in rehearsals that only test clean shutdown paths.
- Affected paths:
  - same-engine upgrades and downgrades
  - cutover windows that require clean source state before final sync
- How to simulate:
  - Open XA transactions and leave them in prepared state.
  - Attempt the upgrade or the final migration checkpoint while they remain unresolved.
- How to detect:
  - Run preflight checks for prepared XA transactions before maintenance.
  - Fail cutover if unresolved distributed transactions exist.
- Mitigation / fix:
  - Resolve or roll back prepared XA transactions before upgrade or final migration.
  - Make this a release-window gate, not a best-effort warning.
- dbmigrate implication:
  - Future operator prechecks should include transaction-state blockers, not only schema compatibility blockers.
- Sources:
  - official: [Preparing your installation for upgrade](https://docs.oracle.com/cd/E17952_01/mysql-8.4-en/upgrade-prerequisites.html)
  - official: [Changes in MySQL 9.6.0](https://dev.mysql.com/doc/relnotes/mysql/9.6/en/news-9-6-0.html)

## 25. Scheduled events and disabled event scheduler drift

- Why it fails:
  - Databases often contain events that are forgotten during migration planning.
  - Event bodies can depend on a definer account, the event scheduler being enabled, and the correct default database or time zone context.
- Affected paths:
  - baseline migration
  - partial database migration
  - post-cutover operational parity
- How to simulate:
  - Create recurring events with `DEFINER` users and target-specific time logic.
  - Restore them with `event_scheduler=OFF` or without the definer account.
- How to detect:
  - Inventory `INFORMATION_SCHEMA.EVENTS` separately from routines and triggers.
  - Report scheduler status on both source and destination.
  - Check whether each event definer exists on the target.
- Mitigation / fix:
  - Migrate event metadata explicitly.
  - Recreate or rewrite missing definers.
  - Validate scheduler state as part of cutover acceptance, not only data consistency.
- dbmigrate implication:
  - Events should be treated as first-class schema objects in compatibility reports.
- Sources:
  - official: [Using the event scheduler](https://dev.mysql.com/doc/refman/8.4/en/event-scheduler.html)
  - official: [Stored object access control](https://dev.mysql.com/doc/refman/en/stored-objects-security.html)

## 26. Time zone tables, named zones, and DST-sensitive verification drift

- Why it fails:
  - Two servers can have matching schemas and rows but still behave differently if time-zone tables are stale or inconsistent.
  - Named time zones and DST transitions can alter computed values, scheduled events, and application-visible timestamps.
- Affected paths:
  - baseline verification
  - event execution after cutover
  - cross-region or mixed-package deployments
- How to simulate:
  - Use named time zones in data conversion or scheduled events.
  - Keep source and destination time-zone tables on different update levels.
  - Compare behavior across DST boundaries.
- How to detect:
  - Record `system_time_zone`, `time_zone`, and whether named time-zone tables are loaded on both sides.
  - Flag mismatches when routines or events use time-zone-aware functions.
- Mitigation / fix:
  - Refresh time-zone tables on both source and destination before verification or cutover.
  - Prefer explicit UTC normalization where application semantics allow it.
- dbmigrate implication:
  - Verification reports may need a note that time-zone drift can cause false positives or post-cutover behavior changes.
- Sources:
  - official: [MySQL time zone support](https://dev.mysql.com/doc/refman/8.4/en/time-zone-support.html)
  - official: [Using the event scheduler](https://dev.mysql.com/doc/refman/8.4/en/event-scheduler.html)

## 27. `SET PERSIST`, persisted variables, and config replay drift

- Why it fails:
  - MySQL supports persisted dynamic system variables and `SET PERSIST`.
  - MariaDB compatibility pages explicitly call this out as a difference.
  - Operators often move schema and data successfully but forget that run-time configuration changes were also part of the old system's behavior.
- Affected paths:
  - same-engine upgrade from configuration-heavy estates
  - `MySQL -> MariaDB` operational cutover
- How to simulate:
  - Persist run-time variables on MySQL.
  - Move workload to MariaDB without equivalent persisted configuration.
  - Compare behavior under load, timeouts, and optimizer knobs.
- How to detect:
  - Inventory persisted variables and non-default dynamic settings before migration.
  - Separate server configuration drift from schema/data drift in reports.
- Mitigation / fix:
  - Capture configuration as migration input, not external tribal knowledge.
  - Re-map or explicitly drop unsupported variables on the target.
- dbmigrate implication:
  - Long-term reporting should grow a config-drift appendix even if dbmigrate does not manage server configs directly.
- Sources:
  - official: [MariaDB 11.0 vs MySQL 8 incompatibilities](https://mariadb.com/docs/release-notes/community-server/about/compatibility-and-differences/incompatibilities-and-feature-differences-between-mariadb-and-mysql-unmaint/incompatibilities-and-feature-differences-between-mariadb-11-0-and-mysql-8)
  - official: [System variable persistence](https://dev.mysql.com/doc/refman/8.4/en/persisted-system-variables.html)

## 28. Binlog expiration, purged logs, and replication error 1236

- Why it fails:
  - Baseline migration or a paused replica can fall behind the source's binlog retention window.
  - Once required binlogs are purged, incremental catch-up fails with `ERROR 1236` and there is no clean replay path from the live source alone.
- Affected paths:
  - baseline plus incremental cutover
  - delayed replicas
  - CDC consumers attached to MySQL/MariaDB binlogs
- How to simulate:
  - Use a short binlog expiration period on the source.
  - Start a baseline copy or stop the replica long enough for required logs to expire.
  - Attempt resume from an old checkpoint.
- How to detect:
  - Record source retention settings before long-running baselines.
  - Compare the checkpoint start file or GTID set against currently available binlogs.
  - Flag long baseline windows when retention is shorter than worst-case copy duration.
- Mitigation / fix:
  - Increase binlog retention before baseline starts.
  - If logs are already gone, rebuild from a fresh backup or fresh baseline rather than trying to skip blindly.
  - Treat expired source history as a hard stop, not a warning.
- dbmigrate implication:
  - Plan and replicate reports should surface retention risk relative to elapsed baseline duration and checkpoint age.
- Sources:
  - official: [PURGE BINARY LOGS](https://dev.mysql.com/doc/refman/8.4/en/purge-binary-logs.html)
  - official: [Troubleshooting replication](https://dev.mysql.com/doc/refman/8.4/en/replication-problems.html)
  - community: [Debezium / MySQL binlogs purged](https://stackoverflow.com/questions/56801816/debezium-failed-cannot-replicate-because-the-master-purged-required-binary-log)
  - community: [GTID auto-position fails after required logs were purged](https://stackoverflow.com/questions/38390765/mysql-error-1236-when-using-gtid)

## 29. Cloned replica identity: duplicate `server_id` or `server_uuid`

- Why it fails:
  - Cloning a data directory or VM image without changing replication identity leaves replicas sharing `server_id` or `server_uuid`.
  - This can produce topology confusion, replication refusal, or hard-to-debug failover behavior.
- Affected paths:
  - same-engine replication
  - DR drills that create replicas from copied volumes
  - migration rehearsals using cloned containers or VMs
- How to simulate:
  - Copy a replica data directory, including `auto.cnf`.
  - Start both original and clone in the same topology.
  - Reuse the same `server_id` intentionally.
- How to detect:
  - Record both `server_id` and `server_uuid` for every node in the topology.
  - On cloned replicas, verify `auto.cnf` regeneration before join.
- Mitigation / fix:
  - Enforce unique `server_id` values for every server.
  - Remove `auto.cnf` on copies so MySQL generates a new `server_uuid`.
  - Treat reused identities as a topology integrity failure.
- dbmigrate implication:
  - Any future replication orchestration or diagnostics should include a topology identity check, not only DSN reachability.
- Sources:
  - official: [Setting the replication source configuration](https://dev.mysql.com/doc/refman/en/replication-howto-masterbaseconfig.html)
  - official: [Adding replicas to a replication environment](https://dev.mysql.com/doc/refman/8.4/en/replication-howto-additionalslaves.html)
  - official: [SHOW REPLICAS](https://dev.mysql.com/doc/refman/8.4/en/show-replicas.html)
  - community: [Duplicate `server_uuid` / `server_id` explained](https://dba.stackexchange.com/questions/285075/what-does-this-error-mean-a-slave-with-the-same-server-uuid-server-id-as-this-s)
  - community: [Changing `server_uuid` by regenerating `auto.cnf`](https://dba.stackexchange.com/questions/334151/change-server-uuid-in-mysql)
  - community: [MySQL thinks source and replica share the same server id](https://dba.stackexchange.com/questions/9756/mysql-thinks-master-slave-have-the-same-server-id)

## 30. Primary-key enforcement, `sql_require_primary_key`, and GIPK drift

- Why it fails:
  - Some managed MySQL environments require a primary key for every replicated table.
  - Dumps that create the table first and add the primary key later with `ALTER TABLE` can fail even when the final schema is valid.
  - MySQL can also generate invisible primary keys, which changes downgrade and failover assumptions.
- Affected paths:
  - import into managed MySQL services
  - same-engine replication with enforced primary-key policy
  - MySQL `8.0.30+` and `9.x` environments using GIPK features
- How to simulate:
  - Create a dump where the table is created without an inline primary key and the PK is added later.
  - Enable `sql_require_primary_key=ON` or use replication channel primary-key enforcement.
  - Repeat with generated invisible primary keys enabled on one side only.
- How to detect:
  - Inspect dump shape, not just final logical schema.
  - Detect tables without explicit primary keys and detect whether GIPK is enabled or generated by policy.
  - Record both `sql_require_primary_key` and replication-channel primary-key enforcement settings where applicable.
  - Flag failover risk when replicas generate independent invisible primary key values.
- Mitigation / fix:
  - Inline primary keys in `CREATE TABLE` for strict targets.
  - Normalize missing-primary-key strategy explicitly: reject, rewrite, or generate.
  - Avoid failing over to replicas whose GIPK values diverge unless that behavior is understood and accepted.
- dbmigrate implication:
  - Primary-key policy needs its own report section, especially when future target environments impose stricter replication requirements.
- Sources:
  - official: [Generated invisible primary keys](https://dev.mysql.com/doc/refman/9.0/en/create-table-gipks.html)
  - official: [CHANGE REPLICATION SOURCE TO](https://dev.mysql.com/doc/refman/8.4/en/change-replication-source-to.html)
  - official: [MySQL Shell copy utilities](https://dev.mysql.com/doc/mysql-shell/8.4/en/mysql-shell-utils-copy.html)
  - official: [MySQL Shell dump loading utility](https://dev.mysql.com/doc/mysql-shell/8.4/en/mysql-shell-utilities-load-dump.html)
  - community: [Import fails when PK is added later and `sql_require_primary_key` is ON](https://stackoverflow.com/questions/75278565/do-refuses-table-import-due-no-primary-key-yet-i-have-a-primary-key)
  - community: [Dump restore FK/PK order issue](https://stackoverflow.com/questions/34133261/mysql-dump-restore-fails-cannot-add-foreign-key-constraint)

## 31. Stale config after upgrade: removed options and startup failure

- Why it fails:
  - Server binaries may upgrade cleanly while startup fails because the old `my.cnf` contains removed or renamed variables.
  - Common examples are query-cache settings, removed auth-plugin options, and legacy startup flags.
- Affected paths:
  - same-engine upgrades
  - image upgrades where config files are mounted from older deployments
  - MySQL `8.4 -> 9.x` or `5.7/8.0 -> 8.4` transitions
- How to simulate:
  - Start a newer MySQL server with an older configuration file containing removed options such as `query_cache_size`, `default-authentication-plugin=mysql_native_password`, or obsolete flags.
  - Repeat with containers that always pull `latest`.
- How to detect:
  - Run an upgrade checker against the source config file before upgrading.
  - Parse config files for removed or deprecated system variables and startup options.
  - Pin image tags so version jumps are explicit, not accidental.
- Mitigation / fix:
  - Treat configuration as part of the migration inventory.
  - Remove or replace unsupported options before starting the new server.
  - Avoid unpinned `latest` images in migration rehearsals and production.
- dbmigrate implication:
  - Even though dbmigrate does not own server bootstrapping, the risk docs should call out config-file drift as a first-class migration blocker.
- Sources:
  - official: [Upgrade checker utility](https://dev.mysql.com/doc/mysql-shell/8.4/en/mysql-shell-utilities-upgrade.html)
  - official: [Option and variable changes for MySQL 8.0](https://dev.mysql.com/doc/mysqld-version-reference/en/optvar-changes-8-0.html)
  - official: [Added, deprecated, or removed in MySQL 8.4](https://dev.mysql.com/doc/refman/8.4/en/added-deprecated-removed.html)
  - community: [Unknown variable `default-authentication-plugin=mysql_native_password`](https://stackoverflow.com/questions/78445419/unknown-variable-default-authentication-plugin-mysql-native-password)
  - community: [Unknown variable `mysql-native-password=ON` after moving to MySQL 9.x](https://stackoverflow.com/questions/78722072/unknown-variable-mysql-native-password-on)
  - community: [Unknown system variable `query_cache_size`](https://stackoverflow.com/questions/49984267/java-sql-sqlexception-unknown-system-variable-query-cache-size)

## 32. Obsolete SQL modes and user-management syntax in old dumps

- Why it fails:
  - Legacy dumps or bootstrap scripts can include SQL modes and account-management syntax removed from newer MySQL lines.
  - Statements that previously emitted warnings can become hard errors after upgrade.
- Affected paths:
  - old schema or bootstrap script replay on newer MySQL
  - cross-engine migration where the source tolerated legacy SQL mode flags
- How to simulate:
  - Replay older dump files or init scripts containing obsolete SQL modes and legacy `GRANT`-based account creation patterns.
  - Compare behavior across MySQL and MariaDB.
- How to detect:
  - Scan dumps and init scripts for obsolete SQL mode flags and removed account-management syntax.
  - Run the MySQL upgrade checker against source definitions and config.
- Mitigation / fix:
  - Rewrite bootstrap scripts to use current `CREATE USER` and `GRANT` forms.
  - Normalize SQL mode to supported flags before migration.
  - Fail fast when init SQL contains removed syntax rather than discovering it at restore time.
- dbmigrate implication:
  - Static prechecks should eventually analyze imported DDL and setup SQL, not only live metadata.
- Sources:
  - official: [Upgrade checker utility](https://dev.mysql.com/doc/mysql-shell/8.4/en/mysql-shell-utilities-upgrade.html)
  - official: [The utf8 character set (deprecated alias for utf8mb3)](https://dev.mysql.com/doc/refman/8.4/en/charset-unicode-utf8.html)
  - official: [Changes in MySQL 8.0.11](https://dev.mysql.com/blog-archive/changes-in-mysql-8-0-11-general-availability/)
  - official: [MariaDB SQL_MODE](https://mariadb.com/kb/en/sql-mode/)
  - vendor: [MariaDB MySQL-to-MariaDB migration compatibility whitepaper](https://mariadb.com/wp-content/uploads/2020/10/mysql-to-mariadb-migration-compatibility_whitepaper_1092.pdf)

## 33. Managed-service migration constraints and hidden platform gaps

- Why it fails:
  - Managed MySQL services often look wire-compatible while quietly restricting system schemas, privileged operations, networking modes, object scope, or replication setup.
  - A migration plan that is safe for self-managed MySQL can fail against Cloud SQL, Azure Database for MySQL, RDS, or Aurora because the platform blocks an assumption rather than the SQL itself.
- Affected paths:
  - self-managed `MySQL/MariaDB -> Cloud SQL`
  - self-managed `MySQL/MariaDB -> Azure Database for MySQL`
  - self-managed `MySQL/MariaDB -> Amazon RDS/Aurora`
- How to simulate:
  - Rehearse migrations into managed targets with non-empty destinations, system-schema dependencies, `DEFINER` objects, and object renames during CDC.
  - Attempt migrations that rely on unsupported storage engines, unsupported privileges, or manual writes to system schemas.
- How to detect:
  - Record target platform type as part of plan input, not just engine/version.
  - Inventory dependencies on `mysql` schema objects, privileged definers, unsupported storage engines, connectivity assumptions, and rename operations during CDC.
  - Check whether the destination must be empty or whether object filtering is restricted by the service.
- Mitigation / fix:
  - Treat managed-service targets as separate compatibility classes.
  - Block plans that depend on platform-forbidden operations.
  - Add explicit operator warnings for service-specific constraints before baseline starts.
- dbmigrate implication:
  - Platform-aware preflight is eventually required if managed destinations are a supported target class.
- Sources:
  - vendor: [Cloud SQL known limitations for MySQL migrations](https://cloud.google.com/database-migration/docs/mysql/known-limitations)
  - vendor: [Cloud SQL for MySQL known issues](https://cloud.google.com/sql/docs/mysql/known-issues)
  - vendor: [Azure DMS replicate changes limitations](https://learn.microsoft.com/en-us/azure/dms/concepts-migrate-azure-mysql-replicate-changes)
  - vendor: [Azure Database for MySQL flexible server limitations](https://learn.microsoft.com/en-us/azure/mysql/flexible-server/concepts-limitations)
  - vendor: [Best practices for migrating to Cloud SQL MySQL](https://cloud.google.com/mysql/migrating)

## 34. Failover and replica restart durability gaps in managed platforms

- Why it fails:
  - Some managed read replicas can become inconsistent or stop replicating after reboot, failover, upgrade, or storage/class change if binlog or redo durability settings were relaxed for performance.
  - This is not a pure MySQL syntax problem; it is an operational replication integrity problem that surfaces during failover or platform events.
- Affected paths:
  - `RDS MySQL` read replicas
  - `Aurora MySQL` binlog replication
  - cutover plans that assume replicas survive platform maintenance without rebuild
- How to simulate:
  - Rehearse failover or replica restart with source durability knobs relaxed.
  - Use high DML load, then force reboot or version change on source or replica.
  - Attempt recovery by skipping errors and observe retention-related secondary failures.
- How to detect:
  - Record `sync_binlog` and `innodb_flush_log_at_trx_commit` on source and replica.
  - Monitor provider-side replication state and lag metrics, not just MySQL thread status.
  - Flag any runbook that relies on repeated error skipping to keep a replica alive.
- Mitigation / fix:
  - For critical windows, use durable settings on the source before failover, upgrade, or replica creation.
  - Prefer recreate-over-skip when the provider documentation says the replica can become inconsistent.
  - Increase binlog retention before attempting large skip/recovery workflows.
- dbmigrate implication:
  - Operator docs should distinguish logical compatibility from replica durability guarantees under platform events.
- Sources:
  - vendor: [Troubleshooting a MySQL read replica problem on Amazon RDS](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/USER_ReadRepl.Troubleshooting.html)
  - vendor: [Aurora MySQL troubleshooting](https://docs.aws.amazon.com/AmazonRDS/latest/AuroraUserGuide/CHAP_Troubleshooting.html)
  - vendor: [Setting up binary log replication for Aurora MySQL](https://docs.aws.amazon.com/AmazonRDS/latest/AuroraUserGuide/AuroraMySQL.Replication.MySQL.SettingUp.html)

## 35. Online migration CDC limitations: rename, DDL timing, and destination emptiness

- Why it fails:
  - Cloud migration services often support only a narrower CDC subset than native MySQL replication.
  - Common traps are destination-must-be-empty rules, unsupported table/database renames during CDC, and DDL during initial dump causing a `table definition has changed` failure.
- Affected paths:
  - Cloud SQL DMS
  - Azure DMS online MySQL migration
  - hybrid self-managed to managed cutovers
- How to simulate:
  - Start an online migration job.
  - Modify table definitions during initial dump.
  - Rename a table or database during CDC.
  - Seed the destination with user objects before starting migration.
- How to detect:
  - Detect non-empty destinations before starting online migration.
  - Detect planned rename operations and DDL windows during baseline plus CDC.
  - Report whether the service requires same database names or specific DDL replication toggles.
- Mitigation / fix:
  - Freeze renames and high-risk DDL during the service's initial dump and CDC phases.
  - Use empty destinations when the service requires it.
  - Move service-specific CDC constraints into the runbook, not tribal memory.
- dbmigrate implication:
  - If dbmigrate is compared against cloud migration services, the docs should explicitly call out where those services are stricter than native replication.
- Sources:
  - vendor: [Diagnose issues for MySQL in Google Database Migration Service](https://cloud.google.com/database-migration/docs/mysql/diagnose-issues)
  - vendor: [Cloud SQL DMS known limitations](https://cloud.google.com/database-migration/docs/mysql/known-limitations)
  - vendor: [Azure DMS replicate changes limitations](https://learn.microsoft.com/en-us/azure/dms/concepts-migrate-azure-mysql-replicate-changes)
  - vendor: [Known issues with migrations to Azure MySQL Flexible Server](https://learn.microsoft.com/en-us/azure/dms/known-issues-azure-mysql-fs-online)

## 36. GTID mode, replica topology, and service-specific replication prerequisites

- Why it fails:
  - Managed services frequently narrow which GTID transitions, topologies, or source types are supported.
  - Operators can have a logically valid MySQL plan that is still invalid on Azure or Cloud SQL because the service requires a narrower setup.
- Affected paths:
  - Azure read replicas
  - Cloud SQL migrations to instances with replicas
  - self-managed source into managed target
- How to simulate:
  - Attempt replica creation with unsupported GTID transition steps.
  - Attempt service migration to a target that already has replicas or unsupported connectivity settings.
  - Attempt replica-of-replica or burstable-tier topologies where the platform disallows them.
- How to detect:
  - Record target service topology constraints before choosing migration shape.
  - Validate GTID mode transitions step-by-step where the service requires them.
  - Detect service tiers and connectivity modes that cannot host the intended replica topology.
- Mitigation / fix:
  - Use platform-supported topology patterns only.
  - Stage GTID changes incrementally if the platform requires one-step transitions.
  - Avoid assuming that "native MySQL supports it" means "the managed service exposes it."
- dbmigrate implication:
  - Future managed-target guidance needs platform-specific prerequisite sections, not just engine/version sections.
- Sources:
  - vendor: [Azure Database for MySQL read replicas](https://learn.microsoft.com/en-us/azure/mysql/flexible-server/concepts-read-replicas)
  - vendor: [Azure Database for MySQL flexible server limitations](https://learn.microsoft.com/en-us/azure/mysql/flexible-server/concepts-limitations)
  - vendor: [Cloud SQL DMS known limitations](https://cloud.google.com/database-migration/docs/mysql/known-limitations)

## 37. DNS caching, connection pools, and false "zero downtime" failovers

- Why it fails:
  - Database failover can complete correctly at the server layer while the application still experiences outage because clients keep stale DNS resolutions or stale pooled connections to the old writer.
  - Managed platforms often market failover as near-zero downtime, but application-visible downtime still occurs if the driver or pool does not refresh endpoints quickly.
- Affected paths:
  - `Aurora MySQL` writer failover
  - `RDS MySQL` Multi-AZ failover
  - any MySQL-compatible cutover behind DNS-based endpoints
- How to simulate:
  - Put a connection pool in front of a writable endpoint resolved via DNS.
  - Force writer failover or endpoint switch.
  - Keep stale connections in the pool and observe write failures after promotion.
- How to detect:
  - Record DNS TTL behavior, driver DNS-refresh behavior, and pool invalidation strategy as part of the runbook.
  - During rehearsal, measure application-visible recovery time, not just provider failover completion time.
  - Flag clients that cache IPs aggressively or do not evict dead pooled connections.
- Mitigation / fix:
  - Use pools and drivers that detect writer change and invalidate stale connections.
  - Keep application-level retry logic for transient writer-switch errors.
  - Where appropriate, use provider proxy layers only after measuring their latency and failover behavior under load.
- dbmigrate implication:
  - Operator docs should explicitly state that successful migration or failover at the server layer does not guarantee application continuity.
- Sources:
  - community: [Aurora Serverless failover still caused downtime because pools kept stale DNS](https://www.reddit.com/r/aws/comments/13fo75p)
  - community: [Aurora failover and DNS caching are not truly zero downtime](https://www.reddit.com/r/aws/comments/11vsvow)
  - community: [Java/.NET style pool issue: refresh DNS after Aurora failover](https://stackoverflow.com/questions/60397514/how-refresh-dns-for-connection-in-the-pool-of-mysql-connector-net)
  - community: [RDS Blue/Green switch still hit DNS cache and stale connections](https://www.reddit.com/r/aws/comments/1rdkgms/database_downtime_under_5_seconds_real_or/)
  - community: [RDS Proxy can reduce failover pain but may increase steady-state latency](https://www.reddit.com/r/aws/comments/rb1ljs)

## 38. Skip-counter "recovery" hides data drift instead of fixing it

- Why it fails:
  - Community incident threads repeatedly show operators clearing replication errors with `sql_replica_skip_counter` or ignore rules, then discovering later that the replica remained logically divergent.
  - Error codes such as `1032` and `1062` usually indicate earlier data drift, missing rows, or non-deterministic apply history, not a one-off harmless event.
- Affected paths:
  - same-engine replication recovery
  - partial-scope replication
  - post-restore catch-up after a bad baseline
- How to simulate:
  - Introduce row drift on the replica.
  - Resume replication until a `1032` or duplicate-key error occurs.
  - Apply skip-counter recovery and verify divergence afterward.
- How to detect:
  - Treat `1032` / `1062` recovery as a corruption signal, not a success path.
  - Compare row-level checksums or targeted table verification after any skipped event.
  - Inspect `performance_schema.replication_applier_status_by_worker` for repeated worker failures rather than only checking that SQL thread restarted.
- Mitigation / fix:
  - Rebuild or resync the affected tables, then re-verify before declaring recovery complete.
  - Use skip-counter only as an emergency containment step with mandatory post-repair validation.
  - Never normalize permanent drift by leaving ignore rules in place.
- dbmigrate implication:
  - Replication-conflict reporting should keep steering operators toward fail-fast plus rebuild, not "skip and hope."
- Sources:
  - community: [Replication broke, skip 1032 looked fine until fresh dump was required](https://www.reddit.com/r/mysql/comments/18uymd9)
  - community: [Partial replication with repeated 1032 failures and no durable fix](https://www.reddit.com/r/DatabaseHelp/comments/1gx9dan)
  - community: [Skipping one event in a transaction can still leave drift; use checksum/sync tools](https://dba.stackexchange.com/questions/95102/what-if-we-skip-the-slave-by-counter-1-when-it-failed-at-event-1-of-a-transactio)
  - community: [Stored procedure replication failure not really fixed by skip counter](https://www.reddit.com/r/mysql/comments/see0ja)

## 39. Crash-unsafe relay metadata and broken replication resume after reboot

- Why it fails:
  - Replica recovery after reboot can fail if relay metadata is file-backed, corrupted, or out of step with source binlog rotation.
  - Real incidents also show permission or binlog-index issues that only surface after restart, which makes the topology look healthy until the first reboot.
- Affected paths:
  - same-engine async replication
  - test topologies built from hand-managed VMs or copied data directories
  - long-running replicas with infrequent restart rehearsals
- How to simulate:
  - Use file-based relay metadata and crash the replica.
  - Rotate binlogs on the source, then reboot source or replica.
  - Introduce bad permissions or mismatched index-file visibility in the lab.
- How to detect:
  - Record whether replication metadata repositories are crash-safe or file-based.
  - Rehearse restart and recovery, not only steady-state replication.
  - Compare the active binlog and relay-log position after restart against expected source state.
- Mitigation / fix:
  - Prefer crash-safe replication metadata repositories where supported.
  - Validate source binlog retention and index-file access before and after restart.
  - Treat reboot-resume failure as a topology design bug, not an operational surprise.
- dbmigrate implication:
  - Future replication guidance should include restart drills as part of acceptance, not just data catch-up.
- Sources:
  - community: [Replication failed to resume correctly after source reboot](https://stackoverflow.com/questions/21499675/why-mysql-replication-fails-to-resume-correctly-after-master-server-is-re-booted)
  - community: [Group replication applier relay-log index missing after restart](https://www.reddit.com/r/mysql/comments/n5fjik)
  - community: [Replication broke when binlog file disappeared after setup mistakes](https://www.reddit.com/r/mysql/comments/hunfht)

## 40. Online DDL tools do not eliminate cut-over and trigger risks

- Why it fails:
  - `pt-online-schema-change` and `gh-ost` reduce lock duration, but they do not abolish risk.
  - Trigger-based copying can add write load, deadlocks, or silent row loss in specific edge cases, while cut-over still requires metadata lock acquisition.
  - Foreign keys, existing triggers, duplicate rows under new unique constraints, and large backfills remain operational traps.
- Affected paths:
  - self-managed MySQL schema change on busy tables
  - replication topologies where online DDL tools are used during CDC windows
  - clustered MySQL variants where DDL interacts with replication or cluster state
- How to simulate:
  - Run `pt-online-schema-change` on a hot table with concurrent writes and foreign keys.
  - Add a unique index where duplicates already exist.
  - Attempt cut-over while long-lived transactions hold metadata locks.
  - Run `gh-ost` rehearsal then cut-over under load.
- How to detect:
  - Inventory existing triggers, foreign keys, and duplicate-key risk before choosing the tool.
  - Measure replication lag and deadlocks during the migration, not only afterward.
  - Require explicit cut-over windows even for "online" tools.
- Mitigation / fix:
  - Choose tool and alter strategy based on table shape, FK layout, and write rate.
  - Use low `lock_wait_timeout`, rehearsed cut-over, and preflight duplicate-key checks.
  - Treat "online" as "lower risk," not "no risk."
- dbmigrate implication:
  - The docs should never imply that external online DDL tools are a general escape hatch; they are separate risk profiles with their own failure modes.
- Sources:
  - official: [pt-online-schema-change documentation](https://docs.percona.com/percona-toolkit/pt-online-schema-change.html)
  - official: [gh-ost project documentation](https://github.com/github/gh-ost)
  - community: [Deadlock encountered when using pt-online-schema-change](https://stackoverflow.com/questions/58871432/deadlock-encountered-when-using-pt-online-schema-change)
  - community: [pt-online-schema-change trigger problem](https://dba.stackexchange.com/questions/184056/pt-online-schema-change-trigger-problem)
  - community: [pt-online-schema-change on large table triggered restart/resource pressure](https://www.reddit.com/r/mysql/comments/1c6fquh)
  - official: [Percona XtraDB Cluster online schema upgrade notes](https://docs.percona.com/percona-xtradb-cluster/8.0/online-schema-upgrade.html)

## 41. Metadata-lock queues turn "one blocked ALTER" into whole-site outage

- Why it fails:
  - A queued DDL waiting for an exclusive metadata lock can cause subsequent ordinary reads and writes to queue behind it.
  - Operators often see "just waiting" and underestimate the blast radius until the app stalls across the site.
- Affected paths:
  - live schema changes on busy tables
  - partition maintenance
  - charset/collation changes
  - cut-over phase of online schema tools
- How to simulate:
  - Open a transaction that reads a target table and keep it open.
  - Issue `ALTER TABLE` or `ALTER DATABASE`.
  - Continue application traffic and observe new sessions pile up behind the pending DDL.
- How to detect:
  - Query `performance_schema.metadata_locks` and processlist during rehearsals.
  - Alert on growing queues of sessions waiting for table or schema metadata locks.
  - Measure impact of queued DDL, not only DDL runtime after it finally starts.
- Mitigation / fix:
  - Keep transactions short and close connections promptly.
  - Use low DDL wait timeouts for production schema changes.
  - Reserve explicit low-traffic windows even when the final DDL is expected to be metadata-only.
- dbmigrate implication:
  - Schema-apply and DDL-policy docs should keep emphasizing that "safe" DDL can still cause operational outages via lock queues.
- Sources:
  - official: [Metadata locking](https://dev.mysql.com/doc/en/metadata-locking.html)
  - community: [ALTER TABLE on active table gives waiting for metadata lock](https://dba.stackexchange.com/questions/49923/mysql-alter-table-on-very-active-table-gives-waiting-for-table-metadata-lock)
  - community: [Queued ALTER blocks later transactions too](https://dba.stackexchange.com/questions/321505/mysql-locking-and-isolation-level)
  - community: [ALTER DATABASE caused waiting for schema metadata lock](https://www.reddit.com/r/mysql/comments/16ax3ls)
  - community: [Busy-table ALTER caused freeze because lock queue formed](https://stackoverflow.com/questions/76125123/mysql-5-7-exclusive-lock-on-table-and-shared-read-locks-causes-deadlock-freeze)

## 42. Foreign-key and partition DDL amplify schema-change outages

- Why it fails:
  - DDL against FK-related or partitioned tables often grabs more metadata locks than operators expect.
  - Online tools also struggle more when foreign keys exist, and partition maintenance can still block in ways that surprise teams expecting "reads should be fine."
- Affected paths:
  - child/parent table schema changes
  - partition maintenance on active tables
  - clustered or replicated environments with partition DDL
- How to simulate:
  - Create parent/child tables with foreign keys and active workload.
  - Create partitioned tables and run `DROP PARTITION` or `ADD PARTITION` during reads and writes.
  - Attempt online schema tools with foreign keys present.
- How to detect:
  - Build FK dependency maps before DDL.
  - Inventory partitioned tables separately and require a more conservative runbook.
  - Monitor metadata locks on both altered and referenced tables.
- Mitigation / fix:
  - Run FK-sensitive and partition DDL in narrower windows.
  - Prefer phased constraint application where possible.
  - Treat partition maintenance as real schema change, not a harmless housekeeping task.
- dbmigrate implication:
  - The backlog should keep FK-sensitive and partition-sensitive cases separate because they fail differently from plain table alters.
- Sources:
  - community: [DROP PARTITION waiting for metadata lock](https://dba.stackexchange.com/questions/35553/avoiding-waiting-for-table-metadata-lock-when-alter-table-drop-partition)
  - community: [Creating FK causes lock on referenced table](https://www.reddit.com/r/mysql/comments/1jp8ba5)
  - community: [Aurora add column stuck because of metadata lock](https://stackoverflow.com/questions/75094484/aurora-mysql-add-column-to-table-getting-stuck)
  - community: [Clustered MariaDB index change disrupts other databases](https://www.reddit.com/r/mariadb/comments/1igh6lm)

## 43. Legacy auth plugins and client-library negotiation failures

- Why it fails:
  - A migration can succeed at the server layer but fail immediately in applications because the client library cannot negotiate the target server's authentication plugin.
  - Community incidents cluster around `caching_sha2_password`, removed `mysql_native_password` assumptions, and old drivers or GUI tools that silently lag the server.
- Affected paths:
  - MySQL `8.x/9.x` cutovers with old application drivers
  - mixed MySQL/MariaDB estates with copied account definitions
  - emergency rollbacks where old clients reconnect to a newer server
- How to simulate:
  - Create business accounts using `caching_sha2_password` and `mysql_native_password`.
  - Connect with older Node, Python, PHP, GUI, and migration-tool clients.
  - Repeat after disabling or removing legacy plugin assumptions from server config.
- How to detect:
  - Inventory application drivers and versions before cutover, not only server plugins.
  - Record which accounts rely on legacy plugins and which clients still depend on them.
  - Include connection smoke tests for representative clients in rehearsal.
- Mitigation / fix:
  - Upgrade client libraries before cutover.
  - Rewrite incompatible accounts or require password resets where appropriate.
  - Avoid using root/system accounts as application identities.
- dbmigrate implication:
  - User/grant compatibility reporting should explicitly call out client-library risk, not just plugin availability on the destination server.
- Sources:
  - community: [Node client does not support server auth protocol after MySQL 8 upgrade](https://stackoverflow.com/questions/79865151/project-cant-connect-to-aws-rds-after-upgrade-sqlstatehy000-2054-the-serv)
  - community: [Node ecosystem workaround discussion for `caching_sha2_password`](https://www.reddit.com/r/node/comments/ssd6vw)
  - community: [MySQL subreddit thread: old client rejected by auth plugin change](https://www.reddit.com/r/mysql/comments/v3bakl)
  - community: [Python connector `caching_sha2_password` not supported](https://www.reddit.com/r/learnpython/comments/ei2nk4)
  - community: [MySQL 5.7 cannot use `caching_sha2_password`](https://www.reddit.com/r/mysql/comments/1jre22d)

## 44. Server collation accepted, client collation unknown

- Why it fails:
  - The server may support a collation perfectly while the client driver or GUI tool cannot map the collation ID or name.
  - That yields confusing failures like "server sent charset unknown to the client" even though SQL-level schema validation looked fine.
- Affected paths:
  - MariaDB `uca1400` deployments with older PHP or GUI clients
  - MySQL `utf8mb4_0900_ai_ci` dumps imported into older MariaDB or older MySQL tooling
  - post-migration application cutovers
- How to simulate:
  - Use `utf8mb4_0900_ai_ci` and `utf8mb4_uca1400_ai_ci` at schema level.
  - Connect using older client libraries, GUI import tools, and PHP drivers.
  - Compare behavior when connection collation differs from storage collation.
- How to detect:
  - Inventory both storage collations and connection-collation expectations.
  - Run representative clients against the target after migration rehearsal.
  - Report collation names that the application stack may not understand, even when the server does.
- Mitigation / fix:
  - Upgrade client libraries or choose a mutually understood connection collation.
  - Map unsupported storage collations before migration where semantics allow it.
  - Keep client compatibility as a separate gate from server compatibility.
- dbmigrate implication:
  - Future reports should distinguish `server-side unsupported` from `client-side unsupported` collation failures.
- Sources:
  - community: [Unknown collation `utf8mb4_0900_ai_ci` on import](https://stackoverflow.com/questions/63995271/i-have-that-error1273-unknown-collation-utf8mb4-0900-ai-ci)
  - community: [DBA SE: MySQL to MariaDB import failed on `utf8mb4_0900_ai_ci`](https://dba.stackexchange.com/questions/248904/mysql-to-mariadb-unknown-collation-utf8mb4-0900-ai-ci)
  - community: [PHP client does not recognize MariaDB `uca1400` collations](https://www.reddit.com/r/PHPhelp/comments/1av88ip)
  - community: [Database subreddit: MariaDB `uca1400` unknown to client](https://www.reddit.com/r/Database/comments/1av8ch1)
  - community: [OpenEMR thread on `utf8mb4_0900_ai_ci` import failure into MariaDB](https://community.open-emr.org/t/docker-capsule-import-failure-due-to-missing-utf8mb4-0900-ai-ci-collation-on-mariadb/23140)

## 45. Dump and import corruption is often toolchain or encoding drift, not just bad SQL

- Why it fails:
  - Operators often blame the server when the real problem is a dump produced or restored with the wrong client binary, wrong character set, too-small packet limits, generated-column mishandling, or binary/NUL bytes in the file path.
  - Community incidents show these failures surfacing as line-1 parse errors, `max_allowed_packet`, `incorrect string value`, or generated-column restore errors.
- Affected paths:
  - logical export/import fallback workflows
  - same-engine restore rehearsals
  - cross-engine dump replay when dbmigrate is bypassed
- How to simulate:
  - Produce dumps with large rows and low `max_allowed_packet`.
  - Restore dumps containing emoji or 4-byte UTF data through clients not configured for `utf8mb4`.
  - Restore dumps that include generated column values with the wrong mysql/mariadb client binary.
  - Attempt to import corrupted or recovered files containing ASCII NUL bytes without `--binary-mode`.
- How to detect:
  - Record dump client version and import client version, not only server versions.
  - Scan for `ASCII '\0'`, BOM/encoding mismatches, generated-column `INSERT` values, and file corruption signs before import.
  - Check packet-size settings and client character-set flags for large or Unicode-heavy dumps.
- Mitigation / fix:
  - Standardize export/import tooling and flags.
  - Use `utf8mb4` consistently end-to-end for Unicode-bearing workloads.
  - Raise `max_allowed_packet` where required and treat binary corruption as file corruption, not SQL syntax.
  - Prefer the correct engine-native dump/load tooling when generated columns are present.
- dbmigrate implication:
  - Operator docs should keep warning that ad hoc dump/import fallbacks can create failure classes dbmigrate’s structured path avoids.
- Sources:
  - community: [mysqldump `max_allowed_packet` failure](https://stackoverflow.com/questions/8815445/mysqldump-error-got-packet-bigger-than-max-allowed-packet)
  - community: [Import size / `max_allowed_packet` workaround](https://stackoverflow.com/questions/9981098/increasing-mysql-import-size)
  - community: [ASCII NUL in recovered dump requires `--binary-mode`](https://stackoverflow.com/questions/26609360/how-to-restore-corrupted-database-file-in-mysql)
  - community: [Generated column restore fails with wrong dump/load path](https://stackoverflow.com/questions/46127855/how-to-restore-mysql-backup-that-have-generated-always-as-column)
  - community: [Reddit: generated columns restore broke when MariaDB mysql binary was used](https://www.reddit.com/r/mysql/comments/uu5lnk)
  - community: [Incorrect string value via JDBC / utf8mb4 mismatch](https://stackoverflow.com/questions/10957238/incorrect-string-value-when-trying-to-insert-utf-8-into-mysql-via-jdbc)
  - community: [Workbench import wizard chokes on ASCII/Unicode handling](https://www.reddit.com/r/mysql/comments/1fw6zv6)

## 46. Backlog classification and execution roadmap for future matrix work

### Locally simulable: ordered execution roadmap

#### Wave 1: High-signal compatibility prechecks

- `unsupported_collations_mysql_to_mariadb`: objects using `utf8mb4_0900_ai_ci`.
- `unsupported_collations_mariadb_to_mysql`: objects using `utf8mb4_uca1400_ai_ci`.
- `auth_plugin_matrix`: business accounts using `caching_sha2_password`, `mysql_native_password`, and MariaDB socket defaults.
- `utf8mb3_alias_upgrade`: explicit `utf8` and `utf8mb3` schemas normalized or rejected on newer MySQL and MariaDB lines.
- `stale_config_upgrade`: boot newer MySQL with removed options from legacy `my.cnf`.
- `obsolete_sql_bootstrap`: init SQL containing removed SQL modes or legacy account syntax.

#### Wave 2: Dump/import correctness and fallback-path hardening

- `dump_tool_skew`: dumps generated by MySQL 8.4 and 9.6 clients with and without `--column-statistics=0` and `--set-gtid-purged=OFF`.
- `dump_packet_limit`: large-row dump or restore with insufficient `max_allowed_packet`.
- `dump_binary_mode_corruption`: corrupted or recovered dump containing ASCII NUL bytes.
- `dump_encoding_drift`: Unicode or emoji-bearing dump restored with wrong client encoding.
- `generated_columns_cross_engine`: dumps and schema extraction for virtual and stored generated columns.
- `sql_require_primary_key_dump`: dump import where PK is added later with `ALTER TABLE`.

#### Wave 3: Schema object and dependency integrity

- `definer_orphan_objects`: views, triggers, procedures, and events whose definer account is intentionally absent on the target.
- `cross_db_partial_scope`: include one schema, omit a referenced schema, then verify object runtime failure.
- `fk_order_replay`: shuffled FK application order and non-standard FK references.
- `event_scheduler_drift`: event definitions with missing definers and `event_scheduler=OFF` on the target.
- `timezone_table_drift`: named-zone and DST-sensitive routines/events with mismatched time-zone tables.
- `persisted_config_drift`: persisted MySQL variables compared against MariaDB target behavior.

#### Wave 4: Replication state and recovery integrity

- `binlog_retention_expiry`: baseline plus replicate resume after source logs have expired.
- `duplicate_replica_identity`: cloned `auto.cnf` and duplicate `server_id` / `server_uuid`.
- `skip_counter_false_recovery`: inject row drift, apply skip-counter recovery, then verify drift remains.
- `relay_metadata_restart`: reboot or crash replica with file-backed relay metadata and validate resume behavior.
- `prepared_xa_blocker`: prepared XA transactions present during final sync or upgrade rehearsal.
- `gipk_divergence`: one side generates invisible primary keys, the other does not.

#### Wave 5: DDL concurrency and online-schema-change hazards

- `metadata_lock_window`: long transaction on source or target while DDL runs.
- `partition_ddl_blocking`: partition maintenance under concurrent workload.
- `online_ddl_cutover_lock`: `pt-online-schema-change` or `gh-ost` cut-over while metadata locks are held.
- `online_ddl_duplicate_loss`: add unique key with pre-existing duplicates under online schema tooling.
- `online_ddl_trigger_conflict`: existing triggers or FK interactions with `pt-online-schema-change`.
- `invisible_columns_downgrade`: MySQL source with invisible columns or indexes.
- `spatial_index_upgrade_gate`: version-gated MySQL object inventory for spatial indexes.

### Cloud-only or cloud-leaning

- `managed_target_scope_gap`: objects or definers that self-managed MySQL allows but Cloud SQL / Azure / RDS block.
- `managed_cdc_rename_block`: rename tables or databases during online migration CDC.
- `managed_destination_not_empty`: online migration against pre-seeded destination.
- `failover_durability_gap`: replica restart or failover with relaxed durability settings.
- `azure_gtid_transition`: unsupported GTID mode transitions and replica topology assumptions.
- `dns_failover_stale_pool`: forced endpoint switch with clients holding stale DNS or pooled writer connections.

### Document unsupported only or low-value to simulate directly

- `managed_proxy_specific_failover`: provider proxy and endpoint behavior that depends on cloud networking layers not represented locally.
- `provider_outage_side_effects`: outages caused by cloud control-plane or storage-layer incidents rather than MySQL semantics.
- `service_tier_limitations`: SKU- or billing-tier restrictions where local simulation adds little value beyond documentation.

## 47. Open research items

- `CLOSED 2026-03-06`: local Phase 62 evidence now covers the highest-value hidden-schema downgrade paths in this repo:
  - `MySQL 8.4 -> MySQL 8.0`: invisible columns and invisible indexes stayed hidden, included dumps preserved GIPK as invisible, and `--skip-generated-invisible-primary-key` removed the hidden PK entirely.
  - `MySQL 8.4 -> MariaDB 10.6`: invisible columns and invisible indexes became visible on restore, included GIPK stopped being invisible, and skipped dumps removed the hidden PK entirely.
  - `MySQL 8.4 -> MariaDB 11.0`: invisible columns and invisible indexes became visible on restore, included GIPK stopped being invisible, and skipped dumps removed the hidden PK entirely.
- `UNCONFIRMED`: the full client-driver blast radius for MariaDB `uca1400` style collations remains incomplete; evidence now includes PHP and Python/mysql-connector style stacks, but not every major Java, Go, and .NET permutation.
- `UNCONFIRMED`: exact transportable-partition-tablespace cross-engine behavior is low-value for dbmigrate's logical path and is likely to remain documentation-only unless a compelling operator case appears.

## 48. Practical reading order for operators

- Read [known-problems.md](./known-problems.md) first for the current enforced safeguards.
- Read [risk-checklist.md](./risk-checklist.md) before planning a real run.
- Use this file when adding new probes, new datasets, or new incompatibility exit paths.

## 49. Verification false positives and checksum/hash mismatch traps

- Why it fails:
  - Operators often assume a mismatched checksum or hash means the data is wrong. That is not always true.
  - False positives appear when the verification method is sensitive to row order, storage format, time zone conversion, floating-point approximation, JSON normalization, or collation-dependent string rendering.
  - `CHECKSUM TABLE` in particular is not a cross-version truth oracle; its result depends on row format and can change after upgrades even when application-visible data still looks equivalent.
- Affected paths:
  - verification after same-engine upgrade or downgrade
  - cross-engine validation
  - replication drift diagnosis
  - fallback scripts that use ad hoc `MD5(GROUP_CONCAT(...))` or `CHECKSUM TABLE`
- How to simulate:
  - Compare `CHECKSUM TABLE` results across versions for tables containing temporal types or changed row formats.
  - Hash the same logical table contents with and without deterministic row ordering.
  - Compare JSON columns before and after MySQL normalization reorders duplicate keys or object members.
  - Verify rows containing `TIMESTAMP`, `FLOAT`, and collation-sensitive text across different session time zones and connection collations.
- How to detect:
  - Record the exact verification primitive in every report: row count, deterministic hash, sample compare, `CHECKSUM TABLE`, or toolkit checksum.
  - Require deterministic ordering and serialization rules for hash-based verification.
  - Record session time zone and connection collation during verify runs.
  - Flag approximate-value columns and JSON columns as higher-risk for strict bytewise mismatch noise.
- Mitigation / fix:
  - Prefer deterministic row-wise hashes over `CHECKSUM TABLE` for release-level validation.
  - Canonicalize ordering, JSON handling, and collation assumptions before hashing.
  - Treat float/timezone/json mismatches as "needs semantic review" before declaring corruption.
  - Re-run suspicious checksum mismatches with table-diff or row-diff verification, not just another checksum.
- dbmigrate implication:
  - Verification reports should distinguish `probable_real_drift` from `possible_false_positive_due_to_representation`.
  - Data-mode docs should explicitly discourage using `CHECKSUM TABLE` as the only evidence for cross-version or cross-engine equality.
- Sources:
  - official: [CHECKSUM TABLE statement](https://dev.mysql.com/doc/refman/9.3/en/checksum-table.html)
  - official: [Replication and floating-point values](https://dev.mysql.com/doc/refman/8.2/en/replication-features-floatvalues.html)
  - official: [DATE, DATETIME, and TIMESTAMP types](https://dev.mysql.com/doc/refman/5.7/en/datetime.html)
  - official: [MySQL JSON normalization](https://dev.mysql.com/doc/refman/8.1/en/json.html)
  - official: [MariaDB vs MySQL compatibility](https://mariadb.com/docs/release-notes/compatibility-and-differences/mariadb-vs-mysql-compatibility)
  - community: [pt-table-checksum reported differences but pt-table-sync found nothing](https://dba.stackexchange.com/questions/94335/pt-table-checksum-says-there-is-a-difference-but-pt-table-sync-with-print-says)
  - community: [CHECKSUM TABLE differed even though tables looked identical](https://stackoverflow.com/questions/59146965/checksum-in-mysql-different)
  - community: [Reddit: replication validation often yields checksum false positives](https://www.reddit.com/r/mysql/comments/yfmhi2)
  - community: [JSON comparison ignores key order in MySQL semantics](https://stackoverflow.com/questions/44767211/mysql-how-to-compare-two-json-objects)

## 50. Data-type edge cases that masquerade as migration or verify failures

- Why it fails:
  - Several "mismatches" are really type-semantics mismatches.
  - `FLOAT` and `DOUBLE` are approximate, `DECIMAL` casting can clamp or round differently than expected, `TIMESTAMP` and `DATETIME` model time differently, JSON gets normalized, and client tools often reinterpret `TINYINT(1)`, `BIT`, or `BOOLEAN`.
- Affected paths:
  - cross-engine verification
  - ETL/import/export tooling
  - client metadata inspection after migration
  - application cutovers using older JDBC/Python/PHP drivers
- How to simulate:
  - Use `FLOAT` and `DOUBLE` with values near rounding thresholds and compare hash outputs.
  - Use `DECIMAL` with many leading zeros or boundary-scale casts.
  - Compare `TIMESTAMP` and `DATETIME` retrieval across different session time zones.
  - Use `TINYINT(1)`, `BIT(1)`, and `BOOLEAN` through JDBC-style clients that treat them differently.
  - Insert JSON documents with duplicate keys or unordered objects, then compare source versus normalized output.
- How to detect:
  - Inventory approximate numeric columns, temporal columns, JSON columns, `BIT`, `ENUM`, and boolean-like columns before verify.
  - Record connector settings such as `tinyInt1isBit` where client metadata drives schema compare behavior.
  - Flag mixed `TIMESTAMP`/`DATETIME` semantics in application-facing columns.
- Mitigation / fix:
  - Use `DECIMAL` instead of `FLOAT` for exact-value business data.
  - Normalize verification for approximate types or compare within a documented tolerance.
  - Prefer UTC discipline and explicit time-zone handling for `TIMESTAMP`.
  - Use explicit connector settings when client metadata would otherwise misclassify booleans or bits.
  - Compare JSON semantically where possible, not as raw text.
- dbmigrate implication:
  - Verification needs a representation-aware mode or at least explicit warnings for approximate numerics, JSON normalization, and connection-timezone effects.
  - Future schema reporting should surface client-metadata interpretation traps for `TINYINT(1)`/`BIT`.
- Sources:
  - official: [Problems with floating-point values](https://dev.mysql.com/doc/refman/5.7/en/problems-with-float.html)
  - official: [Fixed-point types](https://dev.mysql.com/doc/mysql/en/fixed-point-types.html)
  - official: [Connector/J type conversions](https://dev.mysql.com/doc/connector-j/en/connector-j-reference-type-conversions.html)
  - official: [Preserving time instants in Connector/J](https://dev.mysql.com/doc/connectors/en/connector-j-time-instants.html)
  - official: [MySQL JSON data type](https://dev.mysql.com/doc/refman/8.1/en/json.html)
  - official: [Using data types from other database engines](https://dev.mysql.com/doc/refman/en/other-vendor-data-types.html)
  - community: [FLOAT visualization and false precision in MySQL/MariaDB](https://stackoverflow.com/questions/37679274/mysql-mariadb-float-visualization-why-are-values-rounded-to-hundreds-or-thous)
  - community: [MariaDB ROUND confusion caused by floating-point precision](https://stackoverflow.com/questions/67158065/is-this-a-malfunction-of-the-round-function-in-mariadb)
  - community: [Leading-zero DECIMAL cast behaved unexpectedly across MySQL/MariaDB](https://stackoverflow.com/questions/62931303/mysql-and-mariadb-casting-a-string-representing-decimal-value-with-many-leadin)
  - community: [JDBC metadata can reinterpret `TINYINT(1)` as bit/boolean](https://stackoverflow.com/questions/51258496/tinyint-returning-as-bit-in-databasemetadata-java)
  - community: [Sqoop import converted `TINYINT` to boolean](https://stackoverflow.com/questions/37978030/sqoop-import-converting-tinyint-to-boolean)
  - community: [MySQL timezone confusion with `TIMESTAMP`](https://stackoverflow.com/questions/13071287/mysql-time-zone-confusion)
  - community: [Java/MySQL timezone mismatch example](https://stackoverflow.com/questions/1265688/mysql-date-problem-in-different-timezones)

## 51. Resolved or narrowed open items from prior passes

- Invisible columns and GIPK downgrade semantics:
  - Narrowed, not fully closed.
  - Official MySQL documentation now gives two hard facts that matter for downgrade planning:
  - Older versions that do not understand invisible-column versioned comments can recreate invisible columns as visible on reload.
  - GIPKs can be excluded from dumps with `--skip-generated-invisible-primary-key`, and the server setting for generating them is not itself replicated.
  - Practical implication:
  - Logical downgrade tests should explicitly cover `dump with GIPK included`, `dump with GIPK skipped`, and `older target makes invisible visible`.
- Newer MariaDB collations with non-PHP clients:
  - Narrowed.
  - This is no longer just a PHP/mysqlnd issue. Evidence now includes Python/mysql-connector or SQLAlchemy-adjacent stacks and app-level incidents where clients reject server collations or charset IDs they do not know.
  - Practical implication:
  - Keep the client-collation risk as an application-stack compatibility class, not a PHP-only footnote.
- Partition and tablespace combinations:
  - Narrowed.
  - For dbmigrate's logical migration path, physical transportable-tablespace quirks for partitions are low-value to simulate.
  - Practical implication:
  - Keep local simulation focused on partition DDL blocking and logical behavior.
  - Keep transportable tablespace edge cases as `document unsupported only` unless an operator use case forces them into scope.
- Managed failover and DNS practicality:
  - Narrowed.
  - A generic stale-DNS or stale-pool simulation is locally feasible.
  - Cloud control-plane, endpoint, and proxy semantics remain cloud-only.
- Sources:
  - official: [Invisible columns](https://dev.mysql.com/doc/mysql/8.0/en/invisible-columns.html)
  - official: [What is New in MySQL 8.0](https://dev.mysql.com/doc/refman/8.0/en/mysql-nutshell.html)
  - official: [mysqldump](https://dev.mysql.com/doc/refman/8.4/en/mysqldump.html)
  - official: [Generated invisible primary keys](https://dev.mysql.com/doc/refman/9.1/en/create-table-gipks.html)
  - official: [ALTER TABLE partition operations](https://dev.mysql.com/doc/refman/8.2/en/alter-table-partition-operations.html)
  - official: [InnoDB file-per-table tablespaces in MariaDB](https://mariadb.com/kb/en/innodb-file-per-table-tablespaces/)
  - community: [MariaDB 11.3.2 + PHP unknown charset to client](https://stackoverflow.com/questions/78036671/mariadb-11-3-2-php-server-sent-charset-0-unknown-to-the-client-please-rep)
  - community: [App stack hit unknown `utf8mb4_0900_ai_ci` through mysql.connector](https://github.com/Mailu/Mailu/issues/3449)

## 52. Next research queue

These topics are the next strong candidates for additional passes after the current one. They are listed with why they matter and what would count as useful evidence.

- `backup_tool_compatibility`:
  - Why: physical backup/restore tools (`xtrabackup`, `mariabackup`, snapshot-based restore) can fail even when logical migration is clean.
  - Useful evidence: official tool/version matrices plus incident threads on restore or redo-log incompatibility.
- `tls_ssl_transport_breakage`:
  - Why: migrations often fail after cutover because TLS versions, certificate validation, or driver defaults changed.
  - Useful evidence: connector incidents plus official auth/TLS transport notes.
- `routine_view_parser_drift`:
  - Why: views, procedures, and events often fail on parser or reserved-word drift long after table DDL looked fine.
  - Useful evidence: upgrade-checker notes and community breakages with stored objects.
- `blob_text_streaming_limits`:
  - Why: large objects often expose packet-size, client-buffer, and streaming limitations that small test datasets hide.
  - Useful evidence: import/export incidents and connector buffering limits.
- `multi_source_filtering_drift`:
  - Why: multi-source or filtered replication creates silent completeness problems that look healthy in naive status checks.
  - Useful evidence: replication-filter incidents and official topology limitations.

## 53. Seed findings for the next research queue

- `backup_tool_compatibility`:
  - Initial finding:
  - Physical backup tooling is much stricter than logical migration tooling. `mariadb-backup` expects same-version MariaDB server alignment, and MariaDB explicitly recommends `mariadb-backup` instead of Percona XtraBackup for MariaDB `10.3+`. Percona XtraBackup also documents version-check and compatibility boundaries.
  - Why it matters:
  - This is a separate risk class from dbmigrate's logical path and belongs in docs as a sharp distinction.
  - Sources:
    - official: [mariadb-backup overview](https://mariadb.com/kb/en/mariabackup-overview/)
    - official: [Percona XtraBackup version compatibility and server checks](https://docs.percona.com/percona-xtrabackup/8.0/server-backup-version-comparison.html)
    - official: [Percona XtraBackup limitations](https://docs.percona.com/percona-xtrabackup/8.4/limitations.html)
    - community: [mariabackup restore required exact MariaDB version in practice](https://stackoverflow.com/questions/77368469/is-it-possible-to-use-mariabackup-to-backup-a-10-3-db-and-restore-it-to-a-10-6-d)

- `tls_ssl_transport_breakage`:
  - Initial finding:
  - TLS mismatches are an under-documented migration outage source. MySQL `8.4` supports only TLS `1.2` and `1.3`, and failed negotiation terminates the connection rather than falling back cleanly.
  - Why it matters:
  - A server cutover can look perfect while older clients simply cannot establish encrypted sessions.
  - Sources:
    - official: [Encrypted connection TLS protocols and ciphers](https://dev.mysql.com/doc/refman/8.4/en/encrypted-connection-protocols-ciphers.html)
    - official: [MySQL protocol TLS notes](https://dev.mysql.com/doc/dev/mysql-server/latest/page_protocol_basic_tls.html)
    - official: [SSL/TLS improvements and protocol mismatch example](https://dev.mysql.com/blog-archive/ssltls-improvements-in-mysql-5-7-10/)

- `routine_view_parser_drift`:
  - Initial finding:
  - Upgrade-check tooling already warns on reserved words and invalid stored object constructs, which suggests this queue item should focus on stored routines, events, and views rather than tables alone.
  - Why it matters:
  - Parser drift tends to surface later and more selectively than table DDL drift.
  - Sources:
    - official: [MySQL Shell upgrade checker utility](https://dev.mysql.com/doc/mysql-shell/en/mysql-shell-utilities-upgrade.html)
    - official: [Preparing your installation for upgrade](https://docs.oracle.com/cd/E17952_01/mysql-8.4-en/upgrade-prerequisites.html)

- `blob_text_streaming_limits`:
  - Initial finding:
  - Large-object restore failures consistently collapse back to packet size, client buffering, and import-path choice rather than to schema incompatibility.
  - Why it matters:
  - A matrix built only on small fixtures will miss a whole class of operationally relevant dump/import failures.
  - Sources:
    - community: [How to change `max_allowed_packet`](https://stackoverflow.com/questions/8062496/how-to-change-max-allowed-packet-size)
    - community: [Server has gone away when importing large SQL file](https://stackoverflow.com/questions/12425287/mysql-server-has-gone-away-when-importing-large-sql-file)
    - community: [Large import produced inconsistent row counts and packet errors](https://stackoverflow.com/questions/22540266/large-1g-mysql-db-taking-30-hours-to-import-into-wamp-plus-null-errors)

- `multi_source_filtering_drift`:
  - Initial finding:
  - Official MySQL docs explicitly warn that multi-source filtering and GTID diamond topologies can become inconsistent if filtering differs per channel. This is a strong candidate for documentation-first treatment before local simulation.
  - Why it matters:
  - "Replication healthy" can still mean "replicated the wrong subset" in multi-source or filtered topologies.
  - Sources:
    - official: [Replication channel based filters](https://dev.mysql.com/doc/mysql-replication-excerpt/8.0/en/replication-rules-channel-based-filters.html)
    - official: [Replicating different databases to different replicas](https://dev.mysql.com/doc/refman/8.4/en/replication-solutions-partitioning.html)
    - community: [Multi-source replication configuration issue and channel filters](https://stackoverflow.com/questions/56706590/mysql-multi-source-replication-configuration-issue)

## 54. Physical backup tooling is a separate compatibility class from logical migration

- Why it fails:
  - Physical backup tools are much stricter about version matching, storage format, and engine family than logical migration paths.
  - Operators often assume that if SQL is compatible, a physical backup or restore must also be compatible. That assumption is wrong for `mariadb-backup`, `mariabackup`, XtraBackup, and MySQL Enterprise Backup.
  - Community incidents show restores becoming unusable or surprising operators because the backup tool, prepare step, and target server version did not line up exactly.
- Affected paths:
  - MariaDB physical backup and restore across version lines
  - Percona XtraBackup use against MariaDB
  - MySQL Enterprise Backup across minor or release mismatches
- How to simulate:
  - Take a physical backup with one tool version and attempt prepare or restore with another.
  - Restore physical backup data onto a different version line or different engine family.
  - Rehearse "physical backup fallback" alongside logical migration and compare outcomes.
- How to detect:
  - Record backup-tool family and exact version, not just the database version.
  - Distinguish physical restore plans from logical migration plans in operator docs and runbooks.
  - Fail review if the restore plan assumes cross-version or cross-engine portability without vendor support.
- Mitigation / fix:
  - Keep physical backup/restore operations within the supported tool and version matrix.
  - Prefer logical export/import for cross-version or cross-engine movement.
  - Prepare backups with the same tool version that created them where the vendor recommends it.
- dbmigrate implication:
  - The docs should keep stressing that dbmigrate validates logical compatibility; it does not make unsupported physical backup workflows safe.
- Sources:
  - official: [MySQL Enterprise Backup compatibility with MySQL versions](https://dev.mysql.com/doc/mysql-enterprise-backup/8.4/en/bugs.compatibility.html)
  - official: [MariaDB backup and restore overview](https://mariadb.com/docs/server/server-usage/backup-and-restore/backup-and-restore-overview)
  - official: [MariaDB Backup full backup and restore](https://mariadb.com/docs/mariadb-cloud/cloud-data-handling/backup-and-restore/mariadb-backup)
  - official: [mariadb-backup overview](https://mariadb.com/kb/en/mariabackup-overview/)
  - official: [Percona XtraBackup limitations](https://docs.percona.com/percona-xtrabackup/8.4/limitations.html)
  - community: [mariabackup restore across MariaDB versions in practice](https://stackoverflow.com/questions/77368469/is-it-possible-to-use-mariabackup-to-backup-a-10-3-db-and-restore-it-to-a-10-6-d)
  - community: [Physical backup restore corrupted system metadata in field report](https://www.reddit.com/r/mariadb/comments/pzgi2l)

## 55. TLS and SSL transport breakage can make a clean cutover look like an outage

- Why it fails:
  - The server may be healthy after upgrade or migration, but clients fail the TLS handshake because the allowed protocol versions or ciphers no longer overlap.
  - MySQL 8.4 supports TLS `1.2` and `1.3`; if older clients or connectors only speak lower protocols or incompatible ciphers, the connection simply dies.
  - Community incidents repeatedly show operators fixing the "database outage" by downgrading TLS requirements rather than by upgrading clients.
- Affected paths:
  - MySQL `8.4/9.x` cutovers with older drivers
  - MariaDB environments tightened to TLS `1.2` or `1.3`
  - proxy or stunnel-based migration cutovers
- How to simulate:
  - Restrict server TLS to `1.2` and `1.3`.
  - Connect with older drivers or runtime stacks.
  - Rehearse cutover with certificate validation enabled and compare behavior across drivers.
- How to detect:
  - Inventory connector TLS capabilities before cutover.
  - Record `tls_version`, cipher policies, and certificate validation settings as migration inputs.
  - Add representative client TLS smoke tests to the runbook.
- Mitigation / fix:
  - Upgrade clients before tightening TLS policy.
  - Validate trust stores and certificate-hostname expectations ahead of cutover.
  - Treat `--skip-ssl` style workarounds as temporary diagnostics, not as migration completion.
- dbmigrate implication:
  - Connection success from dbmigrate itself is not enough evidence that application clients will survive the cutover.
- Sources:
  - official: [Encrypted connection TLS protocols and ciphers](https://dev.mysql.com/doc/refman/8.4/en/encrypted-connection-protocols-ciphers.html)
  - official: [Configuring MySQL to use encrypted connections](https://dev.mysql.com/doc/refman/8.4/en/using-encrypted-connections.html)
  - official: [MySQL protocol TLS notes](https://dev.mysql.com/doc/dev/mysql-server/latest/page_protocol_basic_tls.html)
  - community: [Python mysqlclient fails when MariaDB is limited to TLS 1.2/1.3](https://stackoverflow.com/questions/58613246/python-mysqlclient-connection-fails-with-tlsv1-2-or-tlsv1-3-configured-on-mariad)
  - community: [Spring/MySQL client broke after TLS tightening](https://stackoverflow.com/questions/56175986/how-can-i-update-my-mysql-connection-through-spring-to-handle-tlsv1-2)
  - community: [Stunnel/MySQL wrong version number during TLS handoff](https://www.reddit.com/r/linuxadmin/comments/1birbpg)

## 56. Routine, view, trigger, and event parser drift surfaces later than table DDL drift

- Why it fails:
  - Table DDL may migrate cleanly while stored routines, views, triggers, or events fail only when parsed against the target version's keyword set and SQL grammar.
  - Upgrade-check tooling explicitly checks stored object syntax because parser drift tends to appear there first.
  - Community incidents also show client tools choking on `INFORMATION_SCHEMA.ROUTINES` or routine metadata even when base tables look fine.
- Affected paths:
  - same-engine upgrades
  - cross-engine logical imports
  - verification after cutover when stored objects are first invoked
- How to simulate:
  - Create routines, views, triggers, and events using identifiers or syntax that became reserved or changed meaning.
  - Restore or upgrade into the target version and invoke the objects.
  - Query metadata through older management tools that inspect routine metadata.
- How to detect:
  - Run upgrade checker syntax checks for routines, triggers, and events.
  - Inventory stored objects separately from tables in preflight.
  - Include stored object invocation and metadata reads in rehearsal.
- Mitigation / fix:
  - Rewrite reserved-word and parser-sensitive stored objects before cutover.
  - Do not treat successful table migration as proof that stored objects are safe.
  - Add stored-object smoke tests to every release rehearsal.
- dbmigrate implication:
  - Stored objects deserve their own compatibility and verification chapter, not a few bullets under generic schema drift.
- Sources:
  - official: [MySQL Shell upgrade checker utility](https://dev.mysql.com/doc/mysql-shell/9.6/en/mysql-shell-utilities-upgrade.html)
  - official: [Preparing your installation for upgrade](https://docs.oracle.com/cd/E17952_01/mysql-8.4-en/upgrade-prerequisites.html)
  - community: [Routine metadata blew up information_schema readers](https://www.reddit.com/r/mariadb/comments/ezqxs6)
  - community: [Upgrade checker output caught stored-object incompatibilities before upgrade](https://www.reddit.com/r/mysql/comments/1joxgv0)

## 57. Large BLOB and TEXT imports fail because of packet, memory, and client-path limits

- Why it fails:
  - Import paths for large objects are often constrained by `max_allowed_packet`, client buffering, language runtime memory, or GUI tooling quirks.
  - The schema may be perfectly valid while the import path cannot stream or buffer the payload safely.
  - Community incidents show large-file imports failing silently, crawling for days, or erroring out only on specific clients.
- Affected paths:
  - logical import fallback workflows
  - application-level BLOB/TEXT writes
  - GUI-based CSV or dump imports
- How to simulate:
  - Insert or import large `BLOB` and `LONGTEXT` values with low `max_allowed_packet`.
  - Use different clients: CLI, Workbench, JDBC, language runtimes.
  - Compare import behavior for large Unicode text and large binary payloads.
- How to detect:
  - Record packet size and import client type before large-object restore.
  - Treat large object size as a migration input, not a post-failure discovery.
  - Include at least one large-payload fixture in the local matrix.
- Mitigation / fix:
  - Raise packet and buffer settings where appropriate.
  - Prefer CLI or streaming-friendly tooling over GUI import wizards for large restores.
  - Consider whether very large objects belong in the database at all for the workload.
- dbmigrate implication:
  - Datasets limited to small values will understate operational risk; the matrix needs at least one deliberately large-payload scenario.
- Sources:
  - official: [Packet too large](https://dev.mysql.com/doc/refman/8.4/en/packet-too-large.html)
  - community: [Packet too large while inserting BLOB](https://stackoverflow.com/questions/22989129/mysql-database-error-packets-larger-than-max-allowed-packet-are-not-allowed-whe)
  - community: [Large data insert cannot dodge `max_allowed_packet`](https://stackoverflow.com/questions/30863484/inserting-big-data-more-than-max-allowed-packet-can-i-dodge-it-somehow)
  - community: [PHP large file insert and blob streaming limitations](https://stackoverflow.com/questions/492549/how-can-i-insert-large-files-in-mysql-db-using-php)
  - community: [Workbench or CSV import paths fail silently or behave badly on large inputs](https://www.reddit.com/r/mysql/comments/zemert)
  - community: [Large imports are painfully slow and packet tuning matters](https://www.reddit.com/r/mysql/comments/1f88ny5)

## 58. Multi-source and filtered replication can look healthy while replicating the wrong subset

- Why it fails:
  - Multi-source replication and channel-based filters create silent completeness problems when channels are configured differently or when operators misunderstand what filtering actually filters.
  - In GTID-based diamond topologies, MySQL explicitly warns that filters must be identical across channels because GTIDs are not filtered out with the row data.
  - Community incidents show operators discovering too late that the desired per-channel filtering simply was not supported on their version.
- Affected paths:
  - multi-source replication
  - filtered replication
  - diamond topologies with GTID
- How to simulate:
  - Configure multiple channels with mismatched filters.
  - Reproduce version-specific behavior where channel-based filtering is unavailable.
  - Validate row completeness, not just replication thread status.
- How to detect:
  - Record all channel-specific filters and compare them for consistency.
  - Treat "replication running" as insufficient evidence in multi-source or filtered setups.
  - Reconcile row counts and sample rows per source channel.
- Mitigation / fix:
  - Keep filter configuration identical across channels where MySQL requires it.
  - Avoid topology assumptions that depend on unsupported per-channel filtering in older versions.
  - Prefer documentation-first treatment unless there is a strong product need to support these setups.
- dbmigrate implication:
  - This is likely a documentation-first area for dbmigrate unless multi-source support becomes an explicit product goal.
- Sources:
  - official: [Replication channel based filters](https://dev.mysql.com/doc/mysql-replication-excerpt/8.0/en/replication-rules-channel-based-filters.html)
  - official: [Replication compatibility between MySQL versions](https://dev.mysql.com/doc/refman/8.4/en/replication-compatibility.html)
  - community: [MySQL multi-source replication configuration issue](https://stackoverflow.com/questions/56706590/mysql-multi-source-replication-configuration-issue)
  - community: [Multi-source replication with identical DB names / filter constraints](https://dba.stackexchange.com/questions/62346/mysql-multi-source-replication-with-identical-db-names)
  - community: [`mysqlbinlog --database` warning: it may filter parts of transactions but not GTIDs](https://stackoverflow.com/questions/60440383/warning-the-option-database-has-been-used)

## 59. Refreshed next research queue

These topics are next after the current pass:

- `ddl_algorithm_and_lock_clause_drift`:
  - Why: `ALGORITHM` and `LOCK` clauses often differ by engine/version and can create false confidence about online schema safety.
- `compression_and_page_format_drift`:
  - Why: compressed row/page formats and storage attributes can break physical restore or produce logical diff noise.
- `replication_applier_parallelism_edge_cases`:
  - Why: parallel apply changes failure shape and ordering assumptions during recovery.
- `connector_metadata_and_orm_drift`:
  - Why: ORMs and connectors misreading metadata can create application-level schema regressions after migration.
- `load_data_infile_and_secure_file_priv`:
  - Why: bulk-load workflows regularly fail on file privilege and path restrictions during restore rehearsals.

## 60. `ALGORITHM` and `LOCK` clauses create false confidence about "online" DDL

- Why it fails:
  - Operators often treat `ALGORITHM=INPLACE` or `LOCK=NONE` as guarantees. They are not.
  - MySQL rejects invalid `ALGORITHM`/`LOCK` combinations, and even valid online operations can still wait on metadata locks or fall back to more disruptive behavior than expected.
  - Instant DDL also has row-version and operation-scope limits, so a rehearsed "instant" change can fail later when table history or operation shape changes.
- Affected paths:
  - same-engine schema changes
  - online migration rehearsal windows
  - manual fallback DDL outside dbmigrate
- How to simulate:
  - Run `ALTER TABLE` with `ALGORITHM=INSTANT`, `INPLACE`, and `LOCK=NONE` across supported and unsupported operations.
  - Hold long transactions open so online DDL must wait for metadata locks.
  - Repeat after enough `INSTANT` column operations to hit row-version limits.
- How to detect:
  - Record requested algorithm and lock clauses in change plans.
  - Preflight whether the specific DDL actually supports the requested online mode.
  - Monitor wait states for metadata locking even when the DDL syntax says `LOCK=NONE`.
- Mitigation / fix:
  - Treat algorithm and lock clauses as requested behavior, not guaranteed behavior.
  - Rehearse exact DDL, not just similar DDL, against representative table history.
  - Keep low lock wait thresholds and explicit rollback plans.
- dbmigrate implication:
  - DDL guidance should avoid shorthand like "online = safe" and instead document operation-by-operation support and lock caveats.
- Sources:
  - official: [InnoDB online DDL operations](https://dev.mysql.com/doc/mysql/8.0/en/innodb-online-ddl-operations.html)
  - official: [Online DDL performance and concurrency](https://dev.mysql.com/doc/refman/5.7/en/innodb-online-ddl-performance.html)
  - official: [MySQL 8.0 instant add column](https://dev.mysql.com/blog-archive/mysql-8-0-innodb-now-supports-instant-add-column/)
  - community: [ALTER TABLE on active table gives waiting for metadata lock](https://dba.stackexchange.com/questions/49923/mysql-alter-table-on-very-active-table-gives-waiting-for-table-metadata-lock)
  - community: [Queued ALTER freezes traffic because of lock queue](https://stackoverflow.com/questions/76125123/mysql-5-7-exclusive-lock-on-table-and-shared-read-locks-causes-deadlock-freeze)

## 61. Compression and page-format features are downgrade and restore traps

- Why it fails:
  - Page compression, compressed row format, `KEY_BLOCK_SIZE`, and redo-log format changes can make a downgrade or physical restore fail even when the logical schema still seems fine.
  - MySQL explicitly warns that page-compressed tables must be uncompressed before downgrading to versions that do not support the feature.
  - Community usage often conflates logical compatibility with page-format compatibility.
- Affected paths:
  - same-engine downgrade
  - physical backup/restore workflows
  - mixed-version storage migration
- How to simulate:
  - Create page-compressed or `ROW_FORMAT=COMPRESSED` tables.
  - Attempt downgrade or restore into older versions.
  - Combine with large `BLOB`/`TEXT` values to surface compressed page and overflow-page behavior.
- How to detect:
  - Inventory compressed tables, page compression, row format, and page size before downgrade planning.
  - Flag downgrade or physical restore plans that cross unsupported compression boundaries.
- Mitigation / fix:
  - Rebuild compressed or page-compressed tables into supported formats before downgrade.
  - Keep physical backup/restore within supported version windows.
  - Separate logical migration approval from physical storage-format approval.
- dbmigrate implication:
  - Compression features should be documented as storage-risk attributes even when dbmigrate's logical copy path can move the data.
- Sources:
  - official: [InnoDB page compression](https://dev.mysql.com/doc/refman/8.4/en/innodb-page-compression.html)
  - official: [InnoDB compression syntax warnings and errors](https://dev.mysql.com/doc/refman/8.0/en/innodb-compression-syntax-warnings.html)
  - official: [InnoDB compression internals](https://dev.mysql.com/doc/refman/8.4/en/innodb-compression-internals.html)
  - official: [Downgrading to a previous MySQL series](https://dev.mysql.com/doc/refman/8.0/en/downgrading-to-previous-series.html)

## 62. Parallel applier and worker gaps complicate replication diagnosis

- Why it fails:
  - Parallel replication improves throughput but changes failure shape.
  - The coordinator can stop because one worker failed, and the replica can temporarily have gaps where the database state never existed on the source.
  - Operators often read only top-level status and miss worker-specific failures, ordering gaps, or commit-order assumptions.
- Affected paths:
  - same-engine async replication
  - group replication
  - multithreaded replica recovery and diagnostics
- How to simulate:
  - Enable `replica_parallel_workers` and create conflicting or dependency-heavy workloads.
  - Force worker-specific failures such as FK or duplicate-key errors.
  - Inspect worker tables in `performance_schema` instead of only `SHOW REPLICA STATUS`.
- How to detect:
  - Record `replica_parallel_workers`, `replica_preserve_commit_order`, and source dependency tracking settings.
  - Query `performance_schema.replication_applier_status_by_worker`.
  - Treat worker failure and sequence gaps as first-class recovery signals.
- Mitigation / fix:
  - Use dependency tracking settings that match the workload.
  - Rebuild or resync when worker failures imply drift, not just restart the coordinator.
  - Document that multithreaded recovery requires worker-level inspection.
- dbmigrate implication:
  - Any future advanced replication diagnostics should surface worker-level failures and gap states directly.
- Sources:
  - official: [Replication threads](https://dev.mysql.com/doc/mysql-replication-excerpt/8.0/en/replication-threads.html)
  - official: [Replication options for replicas](https://dev.mysql.com/doc/mysql/8.0/en/replication-options-replica.html)
  - official: [replication_applier_status_by_worker](https://dev.mysql.com/doc/refman/8.4/en/performance-schema-replication-applier-status-by-worker-table.html)
  - official: [START REPLICA statement](https://dev.mysql.com/doc/refman/8.4/en/start-replica.html)
  - community: [Parallel replication is not working as expected on 5.7 replica](https://dba.stackexchange.com/questions/234209/parallel-replication-is-not-working-on-a-5-7-slave-from-a-5-7-master)
  - community: [Coordinator stopped because worker failed](https://www.reddit.com/r/mysql/comments/18be4ur)
  - community: [Group replication FK worker failure](https://www.reddit.com/r/mysql/comments/181gn5q)

## 63. Connector and ORM metadata drift can create application-level schema regressions

- Why it fails:
  - Different connectors and ORMs reinterpret the same MySQL-family metadata differently, especially around `TINYINT(1)`, `BIT(1)`, booleans, JSON, and MariaDB-vs-MySQL client library selection.
  - A database migration may preserve the table perfectly while the application model changes underneath it because the connector upgraded or because the engine now reports metadata differently.
- Affected paths:
  - JDBC/Hibernate
  - Django/Python ORM
  - SQLAlchemy and mixed MariaDB/MySQL client stacks
  - schema introspection tools and migration frameworks
- How to simulate:
  - Compare metadata inspection before and after driver or engine change for `TINYINT(1)`, `BIT(1)`, JSON, and boolean-like columns.
  - Re-run ORM introspection against both MySQL and MariaDB using different drivers.
  - Test connectors that default to MySQL client libraries against MariaDB servers.
- How to detect:
  - Record connector and ORM versions as part of migration inventory.
  - Include metadata introspection smoke tests, not just query execution.
  - Flag connectors known to reinterpret `TINYINT(1)` or `BIT(1)` by default.
- Mitigation / fix:
  - Pin connector behavior explicitly using documented options such as `tinyInt1isBit` where appropriate.
  - Prefer explicit model declarations over schema introspection defaults when migrating across engines.
  - Separate application metadata validation from raw SQL validation.
- dbmigrate implication:
  - Docs should call out client metadata drift as a post-migration regression source even when dbmigrate's verify passes.
- Sources:
  - official: [Connector/J type conversions](https://dev.mysql.com/doc/connector-j/en/connector-j-reference-type-conversions.html)
  - community: [Why `tinyint(1)` is not treated as boolean with `useInformationSchema=true`](https://stackoverflow.com/questions/59677773/why-tinyint1-isnt-treated-as-boolean-when-useinformationschema-is-true)
  - community: [Django ORM dealing with MySQL `BIT(1)`](https://stackoverflow.com/questions/2706041/django-orm-dealing-with-mysql-bit1-field)
  - community: [pgloader casts `tinyint(1)` to boolean](https://stackoverflow.com/questions/72830182/prevent-pgloader-from-casting-tinyint1-to-boolean)
  - community: [Hibernate/JPA and `tinyint(1)` for boolean](https://stackoverflow.com/questions/3383169/hibernate-jpa-mysql-and-tinyint1-for-boolean-instead-of-bit-or-char)
  - community: [SQLAlchemy uses MySQL client library against MariaDB](https://stackoverflow.com/questions/74539126/sqlalchemy-tries-to-connect-to-mariadb-using-the-mysql-client-library-instead-of)
  - community: [Unknown charset to client after MariaDB upgrade](https://www.reddit.com/r/NextCloud/comments/1awce00)

## 64. `LOAD DATA INFILE`, `LOCAL INFILE`, and `secure_file_priv` break import rehearsals constantly

- Why it fails:
  - Bulk-load workflows are governed by both server-side and client-side restrictions.
  - `secure_file_priv`, `local_infile`, and client-local file restrictions make import behavior differ sharply between server-side `LOAD DATA INFILE` and client-side `LOAD DATA LOCAL INFILE`.
  - GUI tools add another layer of confusion by hiding the exact flags or local file behavior they use.
- Affected paths:
  - CSV import rehearsals
  - mysqlsh copy utilities
  - GUI import workflows
  - fallback bulk-load strategies during migration
- How to simulate:
  - Attempt server-side `LOAD DATA INFILE` with files outside `secure_file_priv`.
  - Attempt `LOAD DATA LOCAL INFILE` with client local loading disabled.
  - Compare CLI, Workbench, and mysqlsh import behavior on the same file.
- How to detect:
  - Record `secure_file_priv` and `local_infile` on both client and server sides.
  - Distinguish server-local and client-local file paths in runbooks and reports.
  - Rehearse bulk-load paths with the actual client tool intended for operations.
- Mitigation / fix:
  - Use `LOAD DATA LOCAL INFILE` only when both sides explicitly allow it and the security tradeoff is accepted.
  - Place server-side files in the approved `secure_file_priv` directory when using non-LOCAL load paths.
  - Prefer mysqlsh copy utilities or controlled CLI workflows over opaque GUI import behavior.
- dbmigrate implication:
  - If operators fall back to `LOAD DATA`, docs should explicitly explain the server/client split and the default security restrictions.
- Sources:
  - official: [LOAD DATA statement](https://dev.mysql.com/doc/refman/8.4/en/load-data.html)
  - official: [Security considerations for LOAD DATA LOCAL](https://dev.mysql.com/doc/refman/en/load-data-local-security.html)
  - official: [MySQL Shell copy utilities](https://dev.mysql.com/doc/mysql-shell/8.4/en/mysql-shell-utils-copy.html)
  - official: [Secure deployment guide: secure_file_priv and local_infile](https://dev.mysql.com/doc/mysql-secure-deployment-guide/8.0/en/secure-deployment-post-install.html)
  - community: [Workbench error 3948 enabling `LOAD DATA LOCAL INFILE`](https://stackoverflow.com/questions/61749842/mysql-workbench8-enable-load-data-local-infilewith-error-code-3948)
  - community: [Error 3948 local data is disabled on both client and server sides](https://stackoverflow.com/questions/66848547/mysql-error-code-3948-loading-local-data-is-disabled-this-must-be-enabled-on-b)
  - community: [`secure_file_priv` error while importing CSV](https://stackoverflow.com/questions/54333344/secure-file-priv-error-while-importing-csv-file-in-sql)
  - community: [Reddit: medium-sized CSV import hit 3948 then 2068](https://www.reddit.com/r/mysql/comments/16c3z18)
  - community: [Workbench import wizard and secure-file/local infile confusion](https://www.reddit.com/r/mysql/comments/1fw6zv6)

## 65. Refreshed next research queue

These topics are next after the current pass:

- `federated_fdw_and_external_table_edges`:
  - Why: cross-server table plugins and foreign wrappers create migration surprises that look like normal table metadata until runtime.
- `generated_column_and_expression_default_drift`:
  - Why: expression defaults and generated-column semantics keep changing across versions and deserve a deeper pass.
- `security_definer_and_role_activation_edges`:
  - Why: invoker/definer and default-role activation behavior can create post-cutover privilege bugs that basic grant export misses.
- `redo_undo_log_capacity_and_long_transaction_recovery`:
  - Why: large transactions and recovery windows can break cutovers even when schema and replication logic are sound.
- `client_shell_gui_tooling_divergence`:
  - Why: mysql CLI, mysqlsh, Workbench, and language drivers often hit different failure classes against the same server.

## 66. External-table and cross-server plugin features rarely survive migration intact

- Why it fails:
  - Features that reach outside the local server boundary, such as `FEDERATED` tables or engine/plugin-based external tables, look like ordinary metadata until runtime proves otherwise.
  - The target may not have the same plugin loaded, remote endpoint reachable, or credentials valid.
  - Even when the DDL imports, the object can remain unusable or semantically wrong after cutover.
- Affected paths:
  - same-engine logical migration with `FEDERATED` tables
  - MariaDB deployments using plugin-backed external tables such as `CONNECT`
  - partial migrations where the remote dependency is intentionally absent
- How to simulate:
  - Create external/federated-style tables that depend on a remote endpoint.
  - Restore the schema without the plugin or without the remote endpoint reachable.
  - Compare DDL success versus runtime query success.
- How to detect:
  - Inventory engine and plugin use, not just table definitions.
  - Flag objects that depend on remote endpoints or plugin-specific capabilities.
  - Treat external connectivity assumptions as migration inputs.
- Mitigation / fix:
  - Fail fast on unsupported external-table features by default.
  - Recreate or redesign these integrations explicitly on the target.
  - Avoid treating plugin-dependent objects as regular tables in reports.
- dbmigrate implication:
  - External-table features should stay in the unsupported or explicit-manual-remediation bucket unless product scope changes.
- Sources:
  - official: [FEDERATED storage engine](https://dev.mysql.com/doc/refman/8.4/en/federated-storage-engine.html)
  - official: [MariaDB CONNECT storage engine](https://mariadb.com/kb/en/connect/)
  - community: [FEDERATED table issue after environment change](https://stackoverflow.com/questions/48702759/mysql-federated-table-issue)
  - community: [CONNECT/external table expectations broke after restore](https://www.reddit.com/r/mariadb/comments/1e8a32e)

## 67. Generated columns and expression defaults drift across versions and engines

- Why it fails:
  - Generated columns, default expressions, and expression-based metadata evolve across versions and are not uniformly portable across MySQL and MariaDB lines.
  - Dumps and schema extractors can serialize the same logical idea differently, and older targets may reject syntax or semantics that newer servers accept.
  - Community incidents consistently show restores failing on generated-column or default-expression details long after base tables looked portable.
- Affected paths:
  - same-engine upgrades and downgrades
  - MySQL <-> MariaDB cross-engine migration
  - dump/import fallback workflows
- How to simulate:
  - Create stored and virtual generated columns plus expression defaults.
  - Dump on newer MySQL or MariaDB and restore into older or different-engine targets.
  - Re-run verification after restore to catch generated-value divergence.
- How to detect:
  - Inventory generated columns and expression defaults separately from ordinary defaults.
  - Flag target versions or engines that do not advertise equivalent expression support.
  - Treat generated-column restore failures as their own incompatibility class.
- Mitigation / fix:
  - Rewrite expression defaults and generated-column syntax before migration where required.
  - Prefer logical transforms over raw dump replay for these objects.
  - Keep generated values out of insert paths unless the target explicitly supports them.
- dbmigrate implication:
  - Generated columns and expression defaults deserve dedicated transforms and prechecks rather than generic parser warnings.
- Sources:
  - official: [Generated columns](https://dev.mysql.com/doc/refman/8.4/en/create-table-generated-columns.html)
  - official: [Data type default values](https://dev.mysql.com/doc/refman/8.4/en/data-type-defaults.html)
  - official: [MariaDB generated columns](https://mariadb.com/kb/en/generated-columns/)
  - community: [Generated columns restore failed](https://stackoverflow.com/questions/46127855/how-to-restore-mysql-backup-that-have-generated-always-as-column)
  - community: [Virtual column import from MySQL to MariaDB failed](https://stackoverflow.com/questions/68008727/error-when-importing-virtual-column-from-mysql-to-mariadb)

## 68. `SQL SECURITY`, definers, and default-role activation create privilege traps after cutover

- Why it fails:
  - Stored objects can execute as `DEFINER` or `INVOKER`, and modern account setups also depend on role activation and default-role state.
  - A migration that copies accounts and routines syntactically can still fail functionally if default roles are not active or if invoker/definer assumptions changed.
  - These bugs often appear only when the application first executes a routine under real permissions.
- Affected paths:
  - user/grant migration
  - routine/view/event cutovers
  - partial account migrations
- How to simulate:
  - Create routines and views with different `SQL SECURITY` modes.
  - Use role-based grants that require default-role activation.
  - Cut over with accounts present but default roles or effective privileges mismatched.
- How to detect:
  - Inventory `SQL SECURITY`, definers, roles, and default-role state together.
  - Test routine execution using application identities, not just administrative accounts.
  - Report effective-privilege drift, not only raw grant syntax.
- Mitigation / fix:
  - Recreate default-role state explicitly on the target.
  - Rewrite or recreate definers where needed.
  - Include execution-time privilege smoke tests in rehearsal.
- dbmigrate implication:
  - Privilege migration must consider effective runtime authorization, not only account and grant text.
- Sources:
  - official: [Stored object access control](https://dev.mysql.com/doc/refman/en/stored-objects-security.html)
  - official: [Using roles](https://dev.mysql.com/doc/refman/8.4/en/roles.html)
  - official: [SET DEFAULT ROLE](https://dev.mysql.com/doc/refman/8.4/en/set-default-role.html)
  - official: [MariaDB roles overview](https://mariadb.com/kb/en/roles_overview/)
  - community: [The user specified as a definer does not exist](https://stackoverflow.com/questions/36223367/the-user-specified-as-a-definer-does-not-exist)
  - community: [Triggers with different users and error 1449](https://stackoverflow.com/questions/35773454/using-triggers-with-different-users-mysql-error-1449)

## 69. Redo/undo capacity and long-transaction recovery can break otherwise sound migration windows

- Why it fails:
  - A migration can be logically correct yet still fail operationally because long transactions exhaust redo or undo capacity, prolong crash recovery, or hold purge back for too long.
  - MySQL 8.0+ exposes redo capacity more explicitly, but operators still underestimate how long-running writes or bulk changes distort recovery windows and replica catch-up behavior.
  - Community incidents regularly reduce to "the schema was fine, but the server was drowning in transaction state."
- Affected paths:
  - cutovers with large batched writes
  - replication catch-up after heavy transactions
  - recovery after crash during large DML or DDL windows
- How to simulate:
  - Run sustained large transactions with heavy write volume.
  - Crash or restart during the write window.
  - Measure recovery time, purge lag, and replica catch-up.
- How to detect:
  - Record redo capacity, undo growth indicators, purge lag, and long-running transaction counts before cutover.
  - Treat extreme transaction size as a migration risk signal, not just an application behavior detail.
  - Include crash/restart rehearsal when large write batches are expected.
- Mitigation / fix:
  - Reduce transaction size during migration windows.
  - Increase redo capacity only with explicit operational review.
  - Schedule cutover around workloads that would otherwise build huge undo and purge debt.
- dbmigrate implication:
  - Runtime guidance should keep emphasizing bounded batches and explicit lag/backpressure controls.
- Sources:
  - official: [Optimizing InnoDB redo logging](https://dev.mysql.com/doc/refman/8.4/en/optimizing-innodb-logging.html)
  - official: [InnoDB undo logs](https://dev.mysql.com/doc/refman/8.4/en/innodb-undo-logs.html)
  - official: [InnoDB recovery](https://dev.mysql.com/doc/refman/8.4/en/innodb-recovery.html)
  - community: [How long would InnoDB recovery take?](https://stackoverflow.com/questions/22614518/how-long-would-it-take-innodb-to-recover)
  - community: [Large transaction and purge lag issues](https://www.reddit.com/r/mysql/comments/10l0w4k)

## 70. mysql CLI, mysqlsh, Workbench, and drivers do not fail the same way

- Why it fails:
  - Operators often test a path with one client and assume another client will behave the same. It will not.
  - `mysql`, `mysqlsh`, Workbench, and language drivers differ on defaults for SSL, local file loading, metadata introspection, import path, and dump/load behavior.
  - This creates "works in CLI" incidents during cutover when the actual operational tool behaves differently.
- Affected paths:
  - import/export rehearsals
  - bulk-load fallbacks
  - post-cutover smoke tests
  - operator runbooks that switch tools mid-incident
- How to simulate:
  - Run the same import, metadata read, and connectivity check through `mysql`, `mysqlsh`, Workbench, and one representative application driver.
  - Compare behavior for SSL, `LOCAL INFILE`, Unicode-heavy input, and metadata introspection.
- How to detect:
  - Record the exact operator tool that will be used in the runbook.
  - Do not accept a CLI-only rehearsal as evidence for a Workbench or driver-based workflow.
  - Include at least one tool-divergence smoke check in migration rehearsals.
- Mitigation / fix:
  - Standardize operational tooling per workflow.
  - Avoid switching tools mid-migration unless the alternate path was rehearsed.
  - Prefer simpler, scriptable clients for critical migration steps.
- dbmigrate implication:
  - The docs should keep steering operators toward explicit, reproducible tool choices rather than generic "import it somehow" guidance.
- Sources:
  - official: [MySQL Shell documentation](https://dev.mysql.com/doc/mysql-shell/8.4/en/)
  - official: [mysql client](https://dev.mysql.com/doc/refman/8.4/en/mysql.html)
  - official: [MySQL Workbench](https://dev.mysql.com/doc/workbench/en/)
  - community: [Workbench import wizard and local infile confusion](https://www.reddit.com/r/mysql/comments/1fw6zv6)
  - community: [Workbench error 3948 enabling `LOAD DATA LOCAL INFILE`](https://stackoverflow.com/questions/61749842/mysql-workbench8-enable-load-data-local-infilewith-error-code-3948)
  - community: [SQLAlchemy tries to use the wrong client library against MariaDB](https://stackoverflow.com/questions/74539126/sqlalchemy-tries-to-connect-to-mariadb-using-the-mysql-client-library-instead-of)

## 71. Refreshed next research queue

These topics are next after the current pass:

- `histogram_statistics_and_optimizer_drift`:
  - Why: optimizer statistics, histograms, and `ANALYZE TABLE` behavior can create post-cutover regressions that look like migration bugs.
- `spatial_gis_and_fulltext_edge_cases`:
  - Why: GIS and FULLTEXT features often have version-specific parser, index, and storage behavior.
- `event_scheduler_and_time_zone_replay_edges`:
  - Why: events plus time zones merit a deeper operational pass beyond the current high-level coverage.
- `password_hash_format_and_account_export_edges`:
  - Why: account export/import remains full of hash-format and plugin subtleties across engines and versions.
- `proxy_pooler_and_connection_router_behavior`:
  - Why: proxies and routers can hide or amplify migration issues differently from direct client connections.

## 72. Histogram and optimizer statistics drift can look like migration breakage even when data is fine

- Why it fails:
  - Query plans can change sharply after migration because histogram statistics, `ANALYZE TABLE`, or optimizer defaults differ, even when the schema and rows are correct.
  - Operators often call this a migration bug because the regression appears immediately after cutover, but the root cause is stale or missing statistics or changed optimizer behavior.
- Affected paths:
  - same-engine upgrades
  - same-dataset cutovers to a fresh target
  - post-restore verification where performance is treated as correctness
- How to simulate:
  - Load identical data on source and target, but refresh histograms or run `ANALYZE TABLE` on only one side.
  - Compare plans and latency before and after histogram updates.
- How to detect:
  - Record histogram presence and `ANALYZE TABLE` timing as part of cutover.
  - Compare execution plans, not just query runtime.
  - Treat performance-only regressions separately from data drift.
- Mitigation / fix:
  - Rebuild or refresh statistics after bulk load or cutover.
  - Freeze or document optimizer-related variables that materially changed.
  - Keep performance smoke tests outside pure data verification.
- dbmigrate implication:
  - Operator docs should explicitly state that verify success does not guarantee stable plans; statistics refresh belongs in the post-cutover runbook.
- Sources:
  - official: [Optimizer statistics](https://dev.mysql.com/doc/refman/8.4/en/optimizer-statistics.html)
  - official: [ANALYZE TABLE statement](https://dev.mysql.com/doc/refman/8.4/en/analyze-table.html)
  - official: [Histogram statistics](https://dev.mysql.com/doc/refman/8.4/en/optimizer-statistics.html#optimizer-statistics-histograms)
  - official: [MariaDB ANALYZE TABLE](https://mariadb.com/kb/en/analyze-table/)

## 73. Spatial/GIS and FULLTEXT features keep their own incompatibility rules

- Why it fails:
  - GIS and FULLTEXT objects have parser, index, and storage rules that differ from ordinary tables.
  - Version-specific changes can make spatial indexes or FULLTEXT parser behavior unsafe across upgrade or downgrade paths even when plain DDL succeeds.
  - Application-visible search or GIS behavior can regress without obvious schema errors.
- Affected paths:
  - same-engine upgrade/downgrade
  - cross-engine migration where GIS or FULLTEXT behavior differs
  - restore rehearsals involving spatial indexes
- How to simulate:
  - Create FULLTEXT indexes with language-specific search patterns.
  - Create spatial indexes and run upgrade/downgrade rehearsal.
  - Compare behavior and not only `SHOW CREATE TABLE`.
- How to detect:
  - Inventory FULLTEXT and spatial indexes separately from ordinary indexes.
  - Flag version pairs with documented GIS or FULLTEXT restrictions.
  - Include query-level smoke tests for search/GIS-heavy workloads.
- Mitigation / fix:
  - Rebuild or validate specialized indexes during upgrade windows.
  - Treat GIS/FULLTEXT workloads as separate test tracks.
  - Do not sign off on these features based only on logical schema migration.
- dbmigrate implication:
  - Specialized index classes should stay outside the "generic table migration succeeded" narrative.
- Sources:
  - official: [Spatial indexes](https://dev.mysql.com/doc/refman/8.4/en/creating-spatial-indexes.html)
  - official: [Full-text restrictions](https://dev.mysql.com/doc/refman/8.4/en/fulltext-restrictions.html)
  - official: [Changes in MySQL 8.4](https://dev.mysql.com/doc/refman/en/upgrading-from-previous-series.html)
  - official: [MariaDB FULLTEXT indexes](https://mariadb.com/kb/en/full-text-index-overview/)

## 74. Event scheduler plus time-zone replay can create delayed post-cutover failures

- Why it fails:
  - Event definitions may restore cleanly but fire at the wrong time, under the wrong time zone, or with the wrong scheduler state after cutover.
  - These failures often appear hours later, which makes them look unrelated to migration.
- Affected paths:
  - event-heavy databases
  - cutovers involving time-zone changes or stale time-zone tables
  - partial migrations where event scheduler state is not validated
- How to simulate:
  - Create scheduled events under named time zones.
  - Restore with stale or mismatched time-zone tables.
  - Enable or disable `event_scheduler` differently on source and target.
- How to detect:
  - Record scheduler state, session time zone, system time zone, and named-zone table readiness.
  - Rehearse event execution after cutover, not only event creation.
  - Treat scheduled-object behavior as a separate acceptance gate.
- Mitigation / fix:
  - Refresh time-zone tables.
  - Validate scheduler state explicitly.
  - Add event-execution smoke checks after cutover.
- dbmigrate implication:
  - Events need post-cutover behavioral checks, not just schema extraction.
- Sources:
  - official: [Using the event scheduler](https://dev.mysql.com/doc/refman/8.4/en/event-scheduler.html)
  - official: [MySQL time zone support](https://dev.mysql.com/doc/refman/8.4/en/time-zone-support.html)
  - official: [MariaDB event scheduler](https://mariadb.com/kb/en/events/)

## 75. Password-hash formats and account-export quirks remain a real migration footgun

- Why it fails:
  - Account export/import is not just `CREATE USER` text. Password hash formats, authentication plugins, and server defaults differ across MySQL and MariaDB lines.
  - Copying raw account metadata or stale grant SQL can silently produce unusable accounts or downgraded security posture.
- Affected paths:
  - same-engine upgrades that change auth defaults
  - MySQL <-> MariaDB account migration
  - rollback plans that expect old client/account behavior
- How to simulate:
  - Export accounts using plugin-specific hashes.
  - Recreate them on the target without normalizing plugins or password handling.
  - Test login with representative clients after migration.
- How to detect:
  - Inventory plugin type, password hash form, TLS requirements, and default-role state together.
  - Separate "account exists" from "account can actually authenticate."
- Mitigation / fix:
  - Recreate accounts using target-supported syntax and plugins.
  - Avoid copying raw system-table rows between engines.
  - Require post-migration auth smoke tests for application identities.
- dbmigrate implication:
  - User/grant migration reporting should keep treating account export as a compatibility problem, not just a metadata copy.
- Sources:
  - official: [Caching SHA-2 authentication](https://dev.mysql.com/doc/refman/en/caching-sha2-pluggable-authentication.html)
  - official: [Native pluggable authentication](https://dev.mysql.com/doc/refman/8.4/en/native-pluggable-authentication.html)
  - official: [Authentication from MariaDB 10.4](https://mariadb.com/docs/server/security/user-account-management/authentication-from-mariadb-10-4)
  - community: [Differences in password hashing between MySQL and MariaDB](https://stackoverflow.com/questions/49300674/differences-in-password-hashing-between-mysql-and-mariadb)
  - community: [Client does not support authentication protocol requested by server](https://stackoverflow.com/questions/51670095/docker-flyway-mysql-8-client-does-not-support-authentication-protocol-requested)

## 76. Proxies, poolers, and routers can hide or amplify migration problems

- Why it fails:
  - Connection routers and proxies change failure shape. They can smooth over failovers or create fresh latency, routing, and stale-connection issues of their own.
  - A migration or failover tested without the real proxy path may not match production behavior at all.
- Affected paths:
  - ProxySQL or HAProxy fronted MySQL
  - cloud proxies and routers
  - any cutover where the app does not connect directly to the server
- How to simulate:
  - Rehearse cutover or failover through the actual proxy or router path.
  - Compare direct connection behavior against proxied behavior under writer switch, TLS change, and metadata reads.
- How to detect:
  - Record whether the application uses a proxy, router, sidecar, or pooler.
  - Treat direct-client rehearsals as insufficient if production uses an intermediate layer.
- Mitigation / fix:
  - Standardize on the production connection path during rehearsal.
  - Measure proxy latency and failure behavior under cutover, not only steady state.
  - Keep direct-connect fallback procedures documented.
- dbmigrate implication:
  - Docs should explicitly call out that direct dbmigrate connectivity does not validate proxy or router semantics.
- Sources:
  - official: [MySQL Router manual](https://dev.mysql.com/doc/mysql-router/8.4/en/)
  - community: [RDS Proxy can help failover but adds tradeoffs](https://www.reddit.com/r/aws/comments/rb1ljs)
  - community: [ProxySQL failover or routing surprises in practice](https://www.reddit.com/r/mysql/comments/13c3o4m)
  - community: [HAProxy MySQL routing discussion](https://stackoverflow.com/questions/39026259/haproxy-and-mysql-failover)

## 77. Refreshed next research queue

These topics are next after the current pass:

- `ddl_copy_algorithm_and_rebuild_costs`:
  - Why: rebuild-heavy DDL needs deeper cost modeling beyond clause support.
- `charset_connection_handshake_drift`:
  - Why: connection-handshake charset negotiation still appears in client incidents and deserves a dedicated pass.
- `trigger_order_and_multiple_trigger_semantics`:
  - Why: trigger execution order and multi-trigger support can create subtle post-cutover behavior drift.
- `checksum_tooling_and_row_canonicalization`:
  - Why: verification remains a high-risk source of false positives without canonical row serialization.
- `replication_filtering_with_views_and_routines`:
  - Why: filtered replication and partial scope can interact badly with stored objects and cross-db references.

## 78. Copy-algorithm DDL and rebuild costs turn "supported" schema change into operational outage

- Why it fails:
  - A DDL operation may be syntactically supported but still require table rebuilds, huge temporary space, or long blocking windows.
  - Operators often stop at "the engine supports this ALTER" and miss the real operational cost of `COPY` behavior, temporary files, and index rebuilds.
- Affected paths:
  - same-engine upgrades with large tables
  - live schema changes outside dbmigrate
  - rollback windows where rebuild time matters
- How to simulate:
  - Run `ALTER TABLE` operations that force `COPY` or full rebuild behavior on large tables.
  - Measure temp-space use, runtime, and blocking under concurrent load.
- How to detect:
  - Preflight whether a given DDL uses `COPY`, `INPLACE`, or `INSTANT`.
  - Record table size, index count, and free disk before applying DDL.
  - Treat rebuild-heavy alters as operational events, not parser checks.
- Mitigation / fix:
  - Schedule rebuild-heavy alters in dedicated windows.
  - Prefer phased or precomputed schema changes where possible.
  - Abort when temp-space and rebuild-cost estimates exceed window limits.
- dbmigrate implication:
  - DDL policy should eventually distinguish low-risk metadata changes from rebuild-heavy changes even when both are "supported."
- Sources:
  - official: [InnoDB online DDL operations](https://dev.mysql.com/doc/mysql/8.0/en/innodb-online-ddl-operations.html)
  - official: [ALTER TABLE statement](https://dev.mysql.com/doc/refman/8.4/en/alter-table.html)
  - community: [ALTER TABLE copy/rebuild pain on large table](https://stackoverflow.com/questions/54667071/mysql-alter-table-copy-locking-performance)

## 79. Connection-handshake charset drift still breaks clients before any query runs

- Why it fails:
  - Some failures happen before SQL execution because the client cannot understand the server’s announced character set or collation during handshake.
  - This looks like an auth or connection bug but is really charset/collation negotiation drift between client and server.
- Affected paths:
  - MariaDB upgrades with newer collations
  - MySQL/MariaDB mixed client stacks
  - cutovers using stale client libraries
- How to simulate:
  - Upgrade server-side defaults or use newer collations.
  - Connect with older client libraries and compare whether the handshake itself fails.
- How to detect:
  - Record client library version and handshake charset support as part of cutover inventory.
  - Treat handshake errors separately from query-time collation errors.
- Mitigation / fix:
  - Upgrade clients or force mutually supported connection character sets where feasible.
  - Separate storage collation choice from connection charset choice when the client stack lags.
- dbmigrate implication:
  - Docs should explicitly distinguish handshake-level incompatibility from schema-level collation incompatibility.
- Sources:
  - official: [Connection character sets and collations](https://dev.mysql.com/doc/refman/8.4/en/charset-connection.html)
  - official: [MariaDB protocol differences with MySQL](https://mariadb.com/docs/server/reference/clientserver-protocol/mariadb-protocol-differences-with-mysql)
  - community: [MariaDB 11.3.2 unknown charset to client](https://stackoverflow.com/questions/78036671/mariadb-11-3-2-php-server-sent-charset-0-unknown-to-the-client-please-rep)
  - community: [App stack hit unknown charset after MariaDB upgrade](https://github.com/Mailu/Mailu/issues/3449)

## 80. Trigger order and multi-trigger semantics can create subtle behavioral drift

- Why it fails:
  - Multiple triggers on the same timing/event, trigger ordering, and implicit assumptions about side effects can change application behavior after migration.
  - These problems rarely surface in table-only verification and instead appear as business-logic drift.
- Affected paths:
  - same-engine upgrades
  - cross-engine migrations with trigger-heavy schemas
  - partial schema cutovers that exclude some supporting routines or tables
- How to simulate:
  - Create multiple triggers on the same table event with ordering dependencies.
  - Restore or replay them across engine/version differences and compare side effects.
- How to detect:
  - Inventory triggers and ordering assumptions separately from base tables.
  - Rehearse write-side business flows that depend on trigger side effects.
- Mitigation / fix:
  - Document and, where possible, simplify trigger chains before migration.
  - Validate business outcomes, not only trigger presence.
- dbmigrate implication:
  - Trigger verification should eventually include behavioral smoke checks or explicit warnings for complex trigger stacks.
- Sources:
  - official: [CREATE TRIGGER statement](https://dev.mysql.com/doc/refman/8.4/en/create-trigger.html)
  - official: [MariaDB trigger overview](https://mariadb.com/kb/en/trigger-overview/)
  - community: [Multiple triggers and ordering assumptions discussion](https://stackoverflow.com/questions/16892070/mysql-same-trigger-time-and-event)

## 81. Checksums are useless without canonical row serialization

- Why it fails:
  - Hash-based validation can lie when row order, JSON rendering, collation, time zone, floating-point formatting, or NULL handling differs between source and destination.
  - Teams often blame replication or migration when the real bug is the checksum algorithm.
- Affected paths:
  - custom verify scripts
  - ad hoc cutover validation
  - cross-engine verification
- How to simulate:
  - Hash the same logical data with different row orderings or different serialization of JSON, NULLs, and temporal values.
  - Compare naive checksum output versus canonical row-wise hashing.
- How to detect:
  - Record the exact serialization used for hashing.
  - Reject validation approaches that do not define deterministic ordering and value formatting.
- Mitigation / fix:
  - Canonicalize row order and value serialization before hashing.
  - Use row-wise diffs or deterministic hashes instead of naive `GROUP_CONCAT` or engine-native checksums alone.
- dbmigrate implication:
  - Verification documentation should keep pushing operators toward deterministic, representation-aware hash modes.
- Sources:
  - official: [CHECKSUM TABLE statement](https://dev.mysql.com/doc/refman/9.3/en/checksum-table.html)
  - community: [pt-table-checksum says there is a difference but sync prints nothing](https://dba.stackexchange.com/questions/94335/pt-table-checksum-says-there-is-a-difference-but-pt-table-sync-with-print-says)
  - community: [CHECKSUM TABLE different on seemingly identical tables](https://stackoverflow.com/questions/59146965/checksum-in-mysql-different)

## 82. Replication filtering plus views and routines creates silent scope holes

- Why it fails:
  - Filters that look correct at the table or database level can still omit data or object dependencies used by views, routines, and cross-database references.
  - The stream looks healthy while the logical application scope is incomplete.
- Affected paths:
  - filtered replication
  - partial database migrations
  - cross-db routine or view dependencies
- How to simulate:
  - Filter one database while routines or views reference another.
  - Verify that replication threads stay healthy while application queries fail or return incomplete results.
- How to detect:
  - Analyze stored object definitions for cross-database references before applying filters.
  - Treat object-scope completeness separately from replication thread health.
- Mitigation / fix:
  - Report filtered-scope dependency holes before migration.
  - Prefer explicit logical extraction scope over naive replication filters when stored objects cross database boundaries.
- dbmigrate implication:
  - Partial-scope support should remain report-heavy and conservative.
- Sources:
  - official: [Replication channel based filters](https://dev.mysql.com/doc/mysql-replication-excerpt/8.0/en/replication-rules-channel-based-filters.html)
  - official: [Stored object access control](https://dev.mysql.com/doc/refman/en/stored-objects-security.html)
  - community: [Replication changes not being sent because of default database behavior](https://stackoverflow.com/questions/5174327/mysql-replication-changes-not-being-sent)

## 83. Refreshed next research queue

These topics are next after the current pass:

- `optimizer_switch_and_sql_mode_behavior_drift`:
  - Why: optimizer and SQL-mode defaults can change result shape or runtime behavior after cutover.
- `replication_error_recovery_runbooks`:
  - Why: operators need a clearer taxonomy of when to rebuild, resync, skip, or fail hard.
- `foreign_key_name_and_constraint_namespace_edges`:
  - Why: constraint naming and namespace rules can still surprise restores and downgrades.
- `bulk_delete_purge_and_archive_patterns`:
  - Why: maintenance deletes and purge/archive jobs often stress migration windows in ways small test datasets miss.
- `observability_and_metric_semantic_drift`:
  - Why: performance_schema, status variables, and monitoring dashboards can shift meaning across versions and engines.

## 84. Optimizer-switch and SQL-mode defaults can change behavior without changing data

- Why it fails:
  - A cutover can preserve schema and rows yet still change query semantics or performance because `optimizer_switch` or `sql_mode` defaults changed.
  - Operators often misclassify this as data corruption when the actual issue is changed parsing, implicit casts, or planner behavior.
- Affected paths:
  - same-engine upgrades
  - MySQL to MariaDB cutovers
  - post-cutover regressions where app behavior changed without data drift
- How to simulate:
  - Run the same workload under different `optimizer_switch` and `sql_mode` settings.
  - Compare results, plans, and errors before and after upgrade.
- How to detect:
  - Inventory effective `sql_mode` and key optimizer settings on source and target.
  - Treat changed result semantics separately from raw data mismatch.
- Mitigation / fix:
  - Normalize critical session or global settings before and after cutover.
  - Add smoke tests for queries known to be sensitive to implicit cast or planner changes.
- dbmigrate implication:
  - Runtime docs should keep emphasizing that compatibility is not just schema plus data; session behavior matters.
- Sources:
  - official: [Server SQL modes](https://dev.mysql.com/doc/refman/8.4/en/sql-mode.html)
  - official: [optimizer_switch system variable](https://dev.mysql.com/doc/refman/8.4/en/switchable-optimizations.html)
  - official: [Improvements to strict mode in MySQL](https://dev.mysql.com/blog-archive/improvements-to-strict-mode-in-mysql/)
  - community: [Query extremely slow after migration to MySQL 5.7](https://stackoverflow.com/questions/37733946/query-extremely-slow-after-migration-to-mysql-5-7)
  - community: [Upgrade caused massive performance issues, likely sql_mode or optimizer_switch](https://www.reddit.com/r/mysql/comments/1klq0b6)
  - community: [SQL_MODE keeps coming back in containers](https://stackoverflow.com/questions/71564069/sql-mode-error-keeps-coming-back-even-after-using-set-sql-mode)

## 85. Replication recovery needs a decision tree, not a bag of ad hoc commands

- Why it fails:
  - Incident threads repeatedly show teams mixing skip counters, replica rebuilds, checksum tools, GTID resets, and relay-log tricks without a clear decision model.
  - Recovery steps that are safe for one failure class are destructive or misleading for another.
- Affected paths:
  - async replication recovery
  - delayed replica catch-up
  - post-failure incident response
- How to simulate:
  - Create different failure classes: missing row, duplicate row, purged binlog, worker failure, and relay-log restart issue.
  - Run different recovery paths and measure whether drift remains.
- How to detect:
  - Classify failures by cause before applying recovery steps.
  - Record whether the issue is data divergence, missing source history, metadata corruption, or transient connectivity.
- Mitigation / fix:
  - Use a documented recovery decision tree: rebuild, resync, skip, or fail hard.
  - Require post-recovery verification before declaring success.
- dbmigrate implication:
  - Conflict and report docs should eventually include recovery-path recommendations tied to failure type.
- Sources:
  - official: [Handling an unexpected halt of a replica](https://dev.mysql.com/doc/mysql-replication-excerpt/8.0/en/replication-solutions-unexpected-replica-halt.html)
  - official: [Replication problems and solutions](https://dev.mysql.com/doc/refman/8.4/en/replication-problems.html)
  - official: [Relay log recovery when SQL thread position is unavailable](https://dev.mysql.com/blog-archive/relay-log-recovery-when-sql-threads-position-is-unavailable/)
  - community: [Master/slave automated resync discussion](https://www.reddit.com/r/mysql/comments/1k10ls7)
  - community: [Replication broke and skip 1032 was the wrong comfort blanket](https://www.reddit.com/r/mysql/comments/18uymd9)
  - vendor: [Aurora migration handbook warning that skipping errors may still require replica rebuild](https://d1.awsstatic.com/whitepapers/Migration/amazon-aurora-migration-handbook.8b8adafbd088c76db0409d5f3685a586e76b0ad5.pdf)

## 86. Foreign-key names and constraint namespaces still break restores and generated DDL

- Why it fails:
  - Constraint names are not always scoped the way operators assume. Duplicate names can break restore or generated DDL even when table definitions look independent.
  - Dump restore or framework-generated schemas can fail on namespace rules long before data copy starts.
- Affected paths:
  - logical restore
  - framework-generated schema replay
  - same-database refactors during migration
- How to simulate:
  - Create foreign keys with colliding constraint names across tables in the same schema.
  - Restore generated DDL into a fresh target and observe duplicate-name failure.
- How to detect:
  - Inventory FK constraint names globally per schema, not only per table.
  - Flag auto-generated naming schemes that are likely to collide.
- Mitigation / fix:
  - Normalize FK names before migration.
  - Use deterministic naming conventions that are globally unique within a schema.
- dbmigrate implication:
  - Schema planning should eventually lint foreign-key names, not only referential structure.
- Sources:
  - community: [Duplicate foreign key constraint name](https://stackoverflow.com/questions/39501899/mysql-duplicate-foreign-key-constraint)
  - community: [Constraint namespace is single within the database](https://www.reddit.com/r/mysql/comments/1bffhmp)
  - community: [Dump restore fails: cannot add foreign key constraint](https://stackoverflow.com/questions/34133261/mysql-dump-restore-fails-cannot-add-foreign-key-constraint)

## 87. Bulk delete, purge, and archive jobs distort migration windows

- Why it fails:
  - Large purge/archive jobs create lag, long transactions, undo debt, metadata contention, and workload spikes that can invalidate otherwise safe migration plans.
  - Teams often discover this only when a one-time cleanup or routine retention job overlaps with cutover.
- Affected paths:
  - cutovers during historical data cleanup
  - replication catch-up with purge-heavy workloads
  - archive-and-delete maintenance jobs
- How to simulate:
  - Run batched deletes and archive moves against large tables during baseline or replication rehearsal.
  - Compare small-batch, partition-drop, and monolithic delete behavior.
- How to detect:
  - Record purge/archive jobs as migration-window blockers.
  - Measure lag, transaction size, and lock impact during rehearsal.
- Mitigation / fix:
  - Pause or redesign purge/archive jobs during migration windows.
  - Prefer partition lifecycle strategies where they fit the data model.
  - Keep delete batches bounded and observable.
- dbmigrate implication:
  - Operational docs should treat maintenance jobs as part of migration readiness, not background noise.
- Sources:
  - community: [Purging large volume of rows](https://www.reddit.com/r/mysql/comments/1kywv5p)
  - community: [Purging records at scale on RDS MySQL](https://www.reddit.com/r/mysql/comments/1kgsw4j)
  - community: [Fastest way to delete or migrate data from a huge table](https://www.reddit.com/r/mysql/comments/1j97dxy)
  - community: [Need help with enormous database, archive then delete](https://www.reddit.com/r/Database/comments/1l3cy4e)

## 88. Monitoring and metric semantics shift across versions and engines

- Why it fails:
  - Dashboards and alerts often assume metric names and meanings stay stable. They do not.
  - `SHOW REPLICA STATUS`, performance schema tables, and status variables evolve across versions, and MariaDB/MySQL do not expose identical instrumentation.
  - An operations team can think the migration degraded the system when part of the problem is that the observability baseline moved.
- Affected paths:
  - same-engine upgrades
  - MySQL vs MariaDB observability comparisons
  - cutovers with existing alerting and dashboards
- How to simulate:
  - Compare monitoring outputs and alerts before and after version or engine change.
  - Re-run incident dashboards against both source and target.
- How to detect:
  - Inventory metric dependencies in dashboards and alerting rules.
  - Record instrumentation differences as part of upgrade planning.
- Mitigation / fix:
  - Revalidate dashboards and alerts after cutover.
  - Keep version-aware observability runbooks for replication and performance metrics.
- dbmigrate implication:
  - Documentation should warn that post-cutover observability drift can look like runtime failure if not re-baselined.
- Sources:
  - official: [SHOW REPLICA STATUS](https://dev.mysql.com/doc/refman/8.4/en/show-replica-status.html)
  - official: [Performance Schema replication tables](https://dev.mysql.com/doc/refman/8.4/en/performance-schema-replication-tables.html)
  - official: [MariaDB SHOW REPLICA STATUS](https://mariadb.com/kb/en/show-replica-status/)

## 89. Refreshed next research queue

These topics are next after the current pass:

- `optimizer_trace_and_hint_drift`:
  - Why: optimizer trace, hints, and `SET_VAR` usage can create upgrade surprises in advanced workloads.
- `spatial_reference_system_and_geometry_encoding_edges`:
  - Why: GIS compatibility goes beyond indexes and needs deeper coverage of geometry formats and SRS handling.
- `event_scheduler_failure_runbooks`:
  - Why: event failures need a clearer operator decision tree similar to replication recovery.
- `account_lock_password_policy_and_expiry_edges`:
  - Why: account usability depends on more than plugin choice or hash format.
- `proxy_failover_consistency_and_read_write_split_edges`:
  - Why: proxies can change read/write routing semantics during failover and cutover.

## 90. Optimizer trace, hints, and `SET_VAR` usage can turn advanced workloads into upgrade surprises

- Why it fails:
  - Advanced applications sometimes pin behavior with optimizer hints, `SET_VAR`, or optimizer-trace-guided tuning. Those controls are version-sensitive and can change meaning or stop helping after migration.
  - The result is a post-cutover query regression that looks data-related but is really hint or optimizer-control drift.
- Affected paths:
  - same-engine upgrades
  - tuned reporting workloads
  - application SQL that uses optimizer hints
- How to simulate:
  - Run representative hinted queries before and after version change.
  - Compare behavior with and without `SET_VAR` and optimizer hints enabled.
  - Inspect optimizer trace and plan choice on both sides.
- How to detect:
  - Inventory use of optimizer hints and session-level tuning hints in application SQL.
  - Treat hint-heavy workloads as a separate validation class during rehearsal.
- Mitigation / fix:
  - Revalidate hint usefulness after upgrade rather than assuming portability.
  - Remove or revise stale hints that lock in bad plans on the target.
- dbmigrate implication:
  - Docs should call out that plan-stabilization techniques can become liabilities across versions.
- Sources:
  - official: [Optimizer hints](https://dev.mysql.com/doc/refman/8.4/en/optimizer-hints.html)
  - official: [optimizer_trace](https://dev.mysql.com/doc/refman/8.4/en/optimizer-trace.html)
  - official: [SET_VAR hint](https://dev.mysql.com/doc/refman/8.4/en/optimizer-hints.html#optimizer-hints-set-var)

## 91. Spatial reference system and geometry encoding compatibility goes beyond indexes

- Why it fails:
  - GIS compatibility is not just about spatial indexes. Spatial reference systems, geometry validity rules, and binary representation details can differ across versions and engines.
  - A migration can preserve the row while changing application-visible GIS behavior or making some geometries invalid under the target rules.
- Affected paths:
  - GIS-heavy workloads
  - same-engine upgrades with spatial features
  - MySQL to MariaDB migrations that use geometry/SRS features
- How to simulate:
  - Create geometries under explicit SRS metadata.
  - Move them across versions or engines and validate both storage and query behavior.
- How to detect:
  - Inventory spatial columns, spatial indexes, and SRS usage separately.
  - Include GIS function smoke tests, not just schema inspection.
- Mitigation / fix:
  - Validate geometry and SRS assumptions during rehearsal.
  - Treat GIS workloads as a dedicated compatibility class.
- dbmigrate implication:
  - Spatial support should remain conservative until query-level and function-level compatibility is well covered.
- Sources:
  - official: [Spatial reference systems](https://dev.mysql.com/doc/refman/8.4/en/spatial-reference-systems.html)
  - official: [Spatial data types](https://dev.mysql.com/doc/refman/8.4/en/spatial-types.html)
  - official: [MariaDB GIS features](https://mariadb.com/kb/en/gis-features-in-533/)

## 92. Event scheduler failures need their own operator runbooks

- Why it fails:
  - Event failures often occur after the migration window and do not resemble schema or data copy failures.
  - Teams need a decision tree for disabled scheduler state, missing definers, bad time-zone data, and runtime execution errors.
- Affected paths:
  - event-heavy databases
  - cutovers involving scheduler-dependent workflows
  - delayed post-cutover task failures
- How to simulate:
  - Run events with missing definers, wrong scheduler state, or stale time-zone tables.
  - Compare execution history and side effects before and after cutover.
- How to detect:
  - Monitor event execution status after cutover, not only event creation.
  - Record scheduler state and definer validity as acceptance criteria.
- Mitigation / fix:
  - Use an explicit event-failure runbook: re-enable scheduler, repair definer, refresh time zones, or re-run task logic.
  - Include event smoke tests in post-cutover validation.
- dbmigrate implication:
  - Event support should be documented as a post-cutover operational track, not just a schema object category.
- Sources:
  - official: [Using the event scheduler](https://dev.mysql.com/doc/refman/8.4/en/event-scheduler.html)
  - official: [Stored object access control](https://dev.mysql.com/doc/refman/en/stored-objects-security.html)
  - official: [MariaDB events](https://mariadb.com/kb/en/events/)

## 93. Account lock, password expiry, and policy state can leave recreated accounts unusable

- Why it fails:
  - A migrated account can exist and still be functionally unusable because of lock state, password expiry, failed-login tracking, or authentication policy differences.
  - Operators often check only that `CREATE USER` succeeded, not that the application identity can actually log in under the new policy regime.
- Affected paths:
  - account migration
  - rollback and cutover testing
  - mixed MySQL/MariaDB auth-policy estates
- How to simulate:
  - Create accounts with password expiry, locked status, or policy constraints.
  - Recreate them on the target and test login with representative clients.
- How to detect:
  - Inventory account lock state, expiry policy, and authentication requirements along with plugin choice.
  - Require real login smoke tests for application identities.
- Mitigation / fix:
  - Recreate policy state deliberately instead of assuming defaults match.
  - Unlock or reset accounts as part of controlled cutover if required.
- dbmigrate implication:
  - User/grant reporting should eventually surface account usability state, not only account definition syntax.
- Sources:
  - official: [Password management](https://dev.mysql.com/doc/refman/8.4/en/password-management.html)
  - official: [ALTER USER](https://dev.mysql.com/doc/refman/8.4/en/alter-user.html)
  - official: [Account locking](https://dev.mysql.com/doc/refman/8.4/en/account-locking.html)
  - official: [MariaDB simple password check plugin](https://mariadb.com/kb/en/authentication-plugin-simple-password-check/)

## 94. Proxy failover and read/write split semantics can invalidate direct-DB rehearsals

- Why it fails:
  - Proxies and routers do more than forward connections. They may cache topology, implement read/write split rules, and route reads and writes differently during or after failover.
  - A cutover that succeeds over direct connections can still fail or become inconsistent through the production proxy path.
- Affected paths:
  - ProxySQL or HAProxy fronted databases
  - MySQL Router topologies
  - cloud routers and managed failover paths
- How to simulate:
  - Rehearse failover or cutover through the actual proxy path with read/write split enabled.
  - Compare application-visible consistency and routing before and after topology change.
- How to detect:
  - Record whether production uses read/write split or topology-aware routing.
  - Validate routing behavior during failover rather than assuming direct-connect outcomes translate.
- Mitigation / fix:
  - Test through the production path.
  - Keep direct-connect fallback procedures and proxy-specific health checks documented.
- dbmigrate implication:
  - Docs should keep repeating that direct dbmigrate connectivity proves only server reachability, not full application routing behavior.
- Sources:
  - official: [MySQL Router manual](https://dev.mysql.com/doc/mysql-router/8.4/en/)
  - community: [HAProxy and MySQL failover discussion](https://stackoverflow.com/questions/39026259/haproxy-and-mysql-failover)
  - community: [ProxySQL failover or routing surprises](https://www.reddit.com/r/mysql/comments/13c3o4m)
  - community: [RDS Proxy tradeoffs in failover scenarios](https://www.reddit.com/r/aws/comments/rb1ljs)

## 95. Refreshed next research queue

These topics are next after the current pass:

- `generated_invisible_primary_key_runtime_edges`:
  - Why: GIPK needs runtime and failover-focused coverage beyond dump and downgrade notes.
- `view_definer_sql_security_and_partial_scope_interactions`:
  - Why: stored-object scope holes deserve a more explicit view/routine-focused pass.
- `large_transaction_chunking_and_autocommit_semantics`:
  - Why: chunking policy and autocommit behavior still drive many practical migration failures.
- `monitoring_agent_and_exporter_compatibility`:
  - Why: exporters and agents can fail differently from direct SQL clients after engine/version changes.
- `engine_specific_non_innodb_table_behavior`:
  - Why: non-InnoDB engines remain a quiet source of restore and runtime surprises.

## 96. Generated invisible primary keys create runtime and failover quirks beyond dump-time concerns

- Why it fails:
  - Generated invisible primary keys are not just a dump or downgrade concern. They also affect runtime assumptions, failover expectations, and tooling that expects explicit primary keys to exist or remain stable.
  - Because GIPKs are server-generated and not always treated like ordinary schema design decisions, different environments can diverge in subtle ways.
- Affected paths:
  - same-engine upgrade/downgrade
  - replica promotion and failover
  - dump and logical replay where GIPKs are included or skipped differently
- How to simulate:
  - Enable GIPK generation on one side and disable it on the other.
  - Compare dump/import and failover behavior with and without `--skip-generated-invisible-primary-key`.
  - Observe how tools and apps behave when a table appears to lack an explicit PK but actually has a hidden one.
- How to detect:
  - Inventory whether GIPK generation is enabled and which tables rely on it.
  - Distinguish explicit primary keys from generated invisible ones in reports.
- Mitigation / fix:
  - Decide explicitly whether GIPKs are acceptable in the target estate.
  - Normalize dumps and restore paths so hidden-key behavior is predictable.
- dbmigrate implication:
  - GIPK should stay a dedicated compatibility class rather than being folded into generic PK handling.
- Sources:
  - official: [Generated invisible primary keys](https://dev.mysql.com/doc/refman/9.1/en/create-table-gipks.html)
  - official: [mysqldump and generated invisible primary keys](https://dev.mysql.com/doc/refman/8.4/en/mysqldump.html)

## 97. Views, definers, `SQL SECURITY`, and partial scope interact badly

- Why it fails:
  - Partial-scope migrations often leave views and routines syntactically present but functionally broken because of missing referenced schemas, missing definers, or `SQL SECURITY` assumptions.
  - These failures can hide until application code touches the object.
- Affected paths:
  - partial database migrations
  - view-heavy schemas
  - user/grant migration disabled or narrowed
- How to simulate:
  - Create views and routines with cross-db references plus `SQL SECURITY DEFINER`.
  - Migrate only part of the schema or omit the definer account.
- How to detect:
  - Analyze cross-db references and definer requirements together.
  - Distinguish object presence from object executability.
- Mitigation / fix:
  - Report missing definer and cross-db dependencies before migration.
  - Require operator acknowledgement for partial scopes that are not self-contained.
- dbmigrate implication:
  - Partial database support should remain conservative and dependency-heavy in reporting.
- Sources:
  - official: [Stored object access control](https://dev.mysql.com/doc/refman/en/stored-objects-security.html)
  - community: [The user specified as a definer does not exist](https://stackoverflow.com/questions/36223367/the-user-specified-as-a-definer-does-not-exist)
  - community: [Triggers with different users and error 1449](https://stackoverflow.com/questions/35773454/using-triggers-with-different-users-mysql-error-1449)

## 98. Chunking strategy and autocommit semantics still decide whether large migrations survive

- Why it fails:
  - Large migrations often fail not because the data is incompatible, but because chunk size, autocommit boundaries, or batching semantics create huge transactions, lock amplification, or restart pain.
  - Operators frequently underestimate how much the same logical work changes risk depending on transaction boundaries.
- Affected paths:
  - baseline data copy
  - large deletes or archive moves
  - replication catch-up and recovery
- How to simulate:
  - Re-run the same workload with different chunk sizes and autocommit behavior.
  - Measure lock duration, recovery time, lag, and restart cost.
- How to detect:
  - Record chunk size, transaction boundaries, and autocommit assumptions as first-class run parameters.
  - Treat "same rows migrated" as insufficient if transaction shape differs radically.
- Mitigation / fix:
  - Keep chunking bounded and deliberate.
  - Prefer restart-safe transaction boundaries over monolithic throughput-maximizing batches.
- dbmigrate implication:
  - Runtime docs should keep focusing on bounded chunking, resumability, and checkpoint-safe transaction edges.
- Sources:
  - official: [AUTOCOMMIT, COMMIT, and ROLLBACK](https://dev.mysql.com/doc/refman/8.4/en/innodb-autocommit-commit-rollback.html)
  - official: [Optimizing InnoDB redo logging](https://dev.mysql.com/doc/refman/8.4/en/optimizing-innodb-logging.html)
  - vendor: [Cloud SQL replication lag guidance](https://docs.cloud.google.com/sql/docs/mysql/replication/replication-lag)

## 99. Monitoring agents and exporters can fail differently from direct SQL clients

- Why it fails:
  - Observability stacks rely on exporters and agents that often use older client libraries, narrower privilege sets, or different metadata queries than applications or dbmigrate itself.
  - After a migration, the app may work while monitoring fails or becomes misleading.
- Affected paths:
  - mysqld_exporter or similar agents
  - legacy monitoring agents
  - post-cutover observability validation
- How to simulate:
  - Test exporters and agents against the target version/engine.
  - Compare exporter query compatibility and privilege requirements before and after cutover.
- How to detect:
  - Inventory monitoring agents and exporter versions in the migration plan.
  - Treat monitoring continuity as a separate acceptance criterion.
- Mitigation / fix:
  - Upgrade exporters and agents before cutover.
  - Revalidate agent privileges and dashboards after migration.
- dbmigrate implication:
  - Docs should mention observability agents explicitly, not just dashboards and humans.
- Sources:
  - community: [Prometheus mysqld_exporter access denied and compatibility gotchas](https://stackoverflow.com/questions/63642618/prometheus-mysql-exporter-access-denied-for-user)
  - community: [mysqld_exporter and MariaDB/MySQL differences discussion](https://www.reddit.com/r/mariadb/comments/17z4e1d)
  - official: [MySQL reference manual privilege system](https://dev.mysql.com/doc/refman/8.4/en/privilege-system.html)

## 100. Non-InnoDB engines still create migration and runtime surprises

- Why it fails:
  - Migration assumptions are usually InnoDB-centric. Non-InnoDB engines such as MyISAM, MEMORY, ARCHIVE, FEDERATED, and Aria have different durability, locking, indexing, or replication behavior.
  - These engines can survive unnoticed in legacy schemas until a restore, verify, or runtime failure exposes them.
- Affected paths:
  - legacy same-engine upgrades
  - cross-engine migrations
  - partial restores and runtime cutovers
- How to simulate:
  - Inventory and migrate schemas that include non-InnoDB engines.
  - Compare locking, durability, and restore behavior versus InnoDB tables.
- How to detect:
  - Inventory storage engine usage as part of schema planning.
  - Flag all non-InnoDB objects explicitly in reports.
- Mitigation / fix:
  - Prefer converting legacy non-InnoDB tables before migration where feasible.
  - Treat engine-specific objects as separate risk classes with dedicated runbook notes.
- dbmigrate implication:
  - Engine inventory should remain a first-class precheck, not a secondary report detail.
- Sources:
  - official: [Alternative storage engines](https://dev.mysql.com/doc/refman/8.4/en/storage-engines.html)
  - official: [MariaDB storage engines](https://mariadb.com/kb/en/storage-engines/)
  - community: [MyISAM to InnoDB conversion and migration pain](https://stackoverflow.com/questions/3111195/convert-all-tables-in-all-databases-to-innodb)

## 101. Refreshed next research queue

These topics are next after the current pass:

- `routine_parser_reserved_word_and_sql_mode_edges`:
  - Why: stored-object parser behavior still deserves a deeper reserved-word and mode-focused pass.
- `temporary_table_and_session_state_edges`:
  - Why: temp tables and session state often break replay or post-cutover behavior in ways schema snapshots miss.
- `gtid_set_surgery_and_reseed_edges`:
  - Why: GTID repair and reseed workflows remain risky and under-documented in real incidents.
- `filesystem_case_symlink_and_path_semantics`:
  - Why: path semantics still matter for case behavior, import paths, and platform portability.
- `replication_privilege_and_channel_user_rotation`:
  - Why: replication users and channel credentials are a recurring operational footgun during upgrades and failover.

## 102. Stored-object parser drift still hides inside reserved words and SQL mode assumptions

- Why it fails:
  - Stored routines, views, triggers, and events can fail even when base tables migrate cleanly because parser behavior and SQL mode handling changed under them.
  - Reserved words, implicit casts, and mode-sensitive expressions are especially dangerous in stored-object bodies.
- Affected paths:
  - same-engine upgrades
  - cross-engine logical imports
  - delayed post-cutover execution of routines or views
- How to simulate:
  - Create routines and views using identifiers or expressions that become mode-sensitive or reserved later.
  - Restore or upgrade into the target and invoke them under the target's SQL mode.
- How to detect:
  - Run stored-object parsing and invocation checks separately from table DDL checks.
  - Inventory SQL mode dependencies in stored objects where possible.
- Mitigation / fix:
  - Rewrite reserved-word and SQL-mode-sensitive stored objects before cutover.
  - Add execution-time smoke tests for critical routines and views.
- dbmigrate implication:
  - Stored-object validation should keep expanding beyond raw existence checks.
- Sources:
  - official: [MySQL Shell upgrade checker utility](https://dev.mysql.com/doc/mysql-shell/9.6/en/mysql-shell-utilities-upgrade.html)
  - official: [Server SQL modes](https://dev.mysql.com/doc/refman/8.4/en/sql-mode.html)
  - official: [Preparing your installation for upgrade](https://docs.oracle.com/cd/E17952_01/mysql-8.4-en/upgrade-prerequisites.html)

## 103. Temporary tables and session state create replay and cutover surprises

- Why it fails:
  - Session-scoped temp tables, user variables, and session state assumptions can make migrations or replay paths behave differently than a static schema snapshot suggests.
  - These issues appear particularly in stored procedures, ETL jobs, and tool-driven cutovers.
- Affected paths:
  - procedure-heavy applications
  - ETL and reporting jobs
  - replay and verification workflows that assume clean stateless execution
- How to simulate:
  - Use routines and application flows that create temp tables or depend on session variables.
  - Re-run them after cutover and compare behavior under different tools or connection pooling.
- How to detect:
  - Inventory temp-table creation and session-variable usage in routines and jobs.
  - Treat session-state assumptions as part of runtime compatibility.
- Mitigation / fix:
  - Rehearse session-dependent jobs explicitly.
  - Avoid assuming connection-pool reuse preserves or resets session state in the same way across environments.
- dbmigrate implication:
  - Session-state-heavy workloads require behavioral smoke tests beyond schema/data copy.
- Sources:
  - official: [CREATE TEMPORARY TABLE statement](https://dev.mysql.com/doc/refman/8.4/en/create-temporary-table.html)
  - official: [User-defined variables](https://dev.mysql.com/doc/refman/8.4/en/user-variables.html)
  - community: [Temporary table scope confusion in stored procedures](https://stackoverflow.com/questions/11835645/dropping-temp-table-at-end-of-stored-procedure-in-mysql)
  - community: [Connection pooling and session variable surprises](https://stackoverflow.com/questions/42158174/mysql-session-variables-vs-connection-pooling)

## 104. GTID set surgery and reseed workflows remain high-risk operator territory

- Why it fails:
  - Repairing GTID state manually is tempting during replication incidents, but bad GTID surgery can permanently confuse topology state or hide divergence.
  - Community incidents often involve `gtid_purged`, `gtid_executed`, or manual reseed operations that appear to fix the topology but leave it semantically unsafe.
- Affected paths:
  - GTID-based replication
  - reseed and rebuild workflows
  - failover and rejoin operations
- How to simulate:
  - Rebuild a replica with GTID enabled and compare safe reseed versus manual GTID edits.
  - Force failure scenarios where source history is missing or replica state diverged.
- How to detect:
  - Treat manual GTID editing as a high-risk recovery class.
  - Record source-of-truth GTID state and compare it before rejoin.
- Mitigation / fix:
  - Prefer documented rebuild/reseed workflows over ad hoc GTID surgery.
  - Require post-reseed validation and topology verification.
- dbmigrate implication:
  - GTID guidance should stay conservative and explicitly steer operators away from unsupported repair shortcuts.
- Sources:
  - official: [GTID auto-positioning](https://dev.mysql.com/doc/refman/8.4/en/replication-gtids-auto-positioning.html)
  - official: [GTID purged and executed set maintenance](https://dev.mysql.com/doc/refman/8.4/en/replication-options-gtids.html)
  - community: [MySQL GTID reset or purged confusion](https://stackoverflow.com/questions/38390765/mysql-error-1236-when-using-gtid)
  - community: [Replica reseed and GTID state discussion](https://dba.stackexchange.com/questions/130425/how-to-reset-mysql-5-6-master-slave-replication-with-gtid)

## 105. Filesystem case, symlink, and path semantics still matter in migration planning

- Why it fails:
  - Path semantics affect case sensitivity, import/export workflows, and data-directory assumptions.
  - Operators often rediscover that a migration crossed filesystem rules only after tables, dump paths, or symlink assumptions stop working.
- Affected paths:
  - platform changes
  - import/export workflows that rely on filesystem paths
  - data-directory or symlink-heavy deployments
- How to simulate:
  - Move mixed-case schemas across environments with different path/case behavior.
  - Rehearse file-based workflows where symlinks or directory assumptions differ.
- How to detect:
  - Record filesystem and `lower_case_table_names` behavior early.
  - Treat path assumptions as part of environment compatibility, not just a deploy detail.
- Mitigation / fix:
  - Normalize naming policies before migration.
  - Avoid brittle symlink/path-dependent workflows in critical cutovers.
- dbmigrate implication:
  - Filesystem semantics should remain first-class in operator readiness checks.
- Sources:
  - official: [Identifier case sensitivity](https://dev.mysql.com/doc/refman/8.4/en/identifier-case-sensitivity.html)
  - official: [Symbolic links in MySQL](https://dev.mysql.com/doc/refman/8.4/en/symbolic-links-to-tables.html)
  - community: [MariaDB and lower_case_table_names migration issue](https://dba.stackexchange.com/questions/346991/mariadb-installation-setting-lower-case-table-names)

## 106. Replication users and channel credential rotation are operational footguns

- Why it fails:
  - Replication users often sit outside normal app-credential rotation processes, so upgrades and failovers expose stale credentials, expired users, wrong auth plugins, or missing privileges.
  - Channel-specific credentials make this worse in multi-source or managed topologies.
- Affected paths:
  - replication setup and failover
  - credential rotation during migration windows
  - multi-channel replication
- How to simulate:
  - Rotate or invalidate replication credentials during rehearsal.
  - Reconnect channels and compare behavior across engine/version changes.
- How to detect:
  - Inventory replication users, channel bindings, privileges, and auth plugins as separate migration inputs.
  - Rehearse credential rotation instead of only initial setup.
- Mitigation / fix:
  - Keep replication-user lifecycle documented and tested.
  - Rotate credentials outside the highest-risk migration moments unless the rotation itself is being rehearsed.
- dbmigrate implication:
  - Replication readiness docs should include credential rotation and channel-user validation, not only binlog settings.
- Sources:
  - official: [Privilege checks for replication users](https://dev.mysql.com/doc/refman/8.4/en/replication-privilege-checks.html)
  - official: [CHANGE REPLICATION SOURCE TO](https://dev.mysql.com/doc/refman/8.4/en/change-replication-source-to.html)
  - official: [MariaDB replication and account requirements](https://mariadb.com/docs/server/ha-and-performance/standard-replication/setting-up-replication)

## 107. Metadata-lock incidents need explicit observability and operator runbooks

- Why it fails:
  - Metadata locks are not just a performance nuisance. A queued DDL can become a traffic amplifier: once it is waiting for an exclusive metadata lock, later readers and writers can pile up behind it and make an application look "frozen".
  - Operators often misdiagnose this as row locking, engine trouble, or network failure because the blocker is frequently an old idle transaction or a long-running read.
- Affected paths:
  - live DDL and online schema change windows
  - cutovers that need final table renames or constraint changes
  - failover or incident periods where metadata lock buildup is already present
- How to simulate:
  - Hold an open transaction on a hot table, then queue `ALTER TABLE` or `RENAME TABLE` from another session.
  - Continue issuing normal app reads and writes to the same object and observe the pileup.
  - Repeat on both MySQL and MariaDB, with and without extra observability enabled.
- How to detect:
  - On MySQL, inspect `performance_schema.metadata_locks` plus worker and processlist state.
  - On MariaDB, enable `performance_schema.metadata_locks` or the `metadata_lock_info` plugin and inspect blocking owners, not just waiters.
  - Capture a runbook view that ties together `SHOW PROCESSLIST`, transaction age, blocking session text, and the specific object name.
- Mitigation / fix:
  - Treat "waiting for table metadata lock" as an incident class with a decision tree: identify blocker, decide whether it is safe to kill the waiting DDL, then decide whether to terminate or drain the blocking transaction.
  - Set conservative session-level wait limits for planned DDL so a queued migration step fails fast instead of amplifying an outage.
  - Rehearse metadata-lock inspection tooling before production DDL windows.
- dbmigrate implication:
  - Migration docs should explicitly warn that even low-impact DDL can deadlock operator expectations through metadata-lock queueing.
  - Future matrix scenarios should include a `metadata_lock_queue_amplification` case with per-engine observability notes and operator-safe abort guidance.
- Sources:
  - official: [MySQL metadata locking](https://dev.mysql.com/doc/en/metadata-locking.html)
  - official: [MySQL Performance Schema metadata_locks table](https://dev.mysql.com/doc/refman/8.4/en/performance-schema-lock-tables.html)
  - official: [MariaDB metadata locking](https://mariadb.com/kb/en/metadata-locking/)
  - official: [MariaDB METADATA_LOCK_INFO plugin](https://mariadb.com/docs/server/reference/plugins/other-plugins/metadata-lock-info-plugin)
  - official: [MariaDB Performance Schema metadata_locks table](https://mariadb.com/docs/server/reference/system-tables/performance-schema/performance-schema-tables/performance-schema-metadata_locks-table)
  - community: [ALTER TABLE queue amplification explanation](https://dba.stackexchange.com/questions/321505/mysql-locking-and-isolation-level)
  - community: [DDL blocked on metadata lock in production](https://dba.stackexchange.com/questions/319278/mysql-table-get-lock-on-adding-new-field)
  - community: [Stuck metadata lock incident on MariaDB](https://dba.stackexchange.com/questions/299368/how-to-remove-a-innodb-metadata-lock-with-thread-id-0-in-mariadb-mysql)

## 108. Successful backup creation still does not prove restore usability

- Why it fails:
  - Backup success often means only "the copy completed", not "the restore is valid, prepared correctly, version-compatible, and queryable".
  - Validation tooling is partial: page checksums can pass while files, metadata, view definitions, plugin dependencies, or restore procedure steps are still wrong.
  - Physical backup tools add their own version and prepare-step constraints, so a backup can exist and still be operationally useless at restore time.
- Affected paths:
  - physical backup assisted migrations
  - emergency rollback plans
  - pre-cutover safety claims based only on successful backup jobs
- How to simulate:
  - Create backups with both logical and physical tooling, then rehearse full restore on a clean host or container.
  - Intentionally skip prepare/apply-log steps for MariaDB or use mismatched backup-tool versions across major versions.
  - Validate that post-restore reads, routine invocation, and account or plugin dependencies still work.
- How to detect:
  - Separate "backup completed" from "backup validated" and from "backup restored and smoke-tested".
  - Record tool version, source version, prepare step, restore step, and post-restore smoke test as distinct gates.
  - Inventory unsupported or partially checked file types and object classes in the chosen backup tool.
- Mitigation / fix:
  - Make restore rehearsal mandatory for release-grade migration confidence.
  - For physical backups, require documented prepare/apply-log steps and tool-version compatibility, not just possession of a backup directory or image.
  - For logical dumps, run restore smoke tests that cover views, routines, grants, and at least representative data access paths.
- dbmigrate implication:
  - Operator docs should never treat backup existence as sufficient rollback assurance.
  - Future matrix scenarios should include `backup_restore_rehearsal_required`, `physical_backup_prepare_missing`, and `backup_tool_version_mismatch`.
- Sources:
  - official: [Using backups for recovery](https://dev.mysql.com/doc/refman/8.4/en/recovery-from-backups.html)
  - official: [MySQL Enterprise Backup validation operations](https://dev.mysql.com/doc/mysql-enterprise-backup/8.0/en/backup-commands-validate.html)
  - official: [MySQL Enterprise Backup verifying a backup](https://dev.mysql.com/doc/mysql-enterprise-backup/8.0/en/mysqlbackup.verify.html)
  - official: [MySQL Enterprise Backup apply-log and restore](https://dev.mysql.com/doc/mysql-enterprise-backup/8.4/en/backup-apply-log.html)
  - official: [mariadb-backup options and prepare requirement](https://mariadb.com/kb/en/mariabackup-options/)
  - community: [XtraBackup version mismatch breaks restore path](https://forums.percona.com/t/xtrabackup-8-0-failed-to-restore-backup-from-mysql5-7/23224)
  - community: [Intermittent backup success is not proof of good restore state](https://forums.percona.com/t/percona-xtrabackup-intermediately-failing/24228)
  - community: [Dump restore can still fail on tablespace assumptions](https://stackoverflow.com/questions/17914446/mysqldump-problems-with-restore-error-please-discard-the-tablespace-before-imp)

## 109. Session time zone and NOW-like functions still create quiet behavior drift

- Why it fails:
  - Time and date functions are affected by session time zone, system time zone, function semantics, and column type semantics.
  - `NOW()` is replication-aware, but that does not remove drift if source and replica use different effective time-zone settings or if the application compares `DATETIME` and `TIMESTAMP` behavior inconsistently.
  - `SYSDATE()` and local-time assumptions remain common field failures, especially after cutover to a host or region with different defaults.
- Affected paths:
  - replication across regions or different host time zones
  - cutovers that change server locale or session-init behavior
  - apps using `NOW()`, `CURRENT_TIMESTAMP`, `FROM_UNIXTIME()`, `CONVERT_TZ()`, or mixed `TIMESTAMP` and `DATETIME` semantics
- How to simulate:
  - Run source and target with different `system_time_zone` and `time_zone` combinations, including `SYSTEM`.
  - Compare inserts into `TIMESTAMP` and `DATETIME` columns under session-level time-zone changes.
  - Rehearse DST-adjacent timestamps and application connections that set session time zone differently from the server default.
- How to detect:
  - Capture `@@system_time_zone`, `@@global.time_zone`, and representative session `time_zone` settings as migration inputs.
  - Inventory use of time-sensitive functions, especially in defaults, triggers, ETL, or verification queries.
  - Detect timezone-table dependency if named zones are used with `CONVERT_TZ()`.
- Mitigation / fix:
  - Standardize on UTC where possible, and be explicit about session initialization.
  - Use `TIMESTAMP` versus `DATETIME` intentionally and document the consequence for storage and display.
  - Treat DST and local-time assumptions as behavioral compatibility checks, not just configuration trivia.
- dbmigrate implication:
  - Prechecks should eventually surface time-zone-sensitive defaults, function usage, and mixed column semantics as a separate compatibility class.
  - Future matrix scenarios should include `timezone_session_drift`, `timestamp_vs_datetime_semantics`, and `dst_cutover_behavior`.
- Sources:
  - official: [Replication and system functions](https://dev.mysql.com/doc/refman/8.4/en/replication-features-functions.html)
  - official: [Replication and time zones](https://dev.mysql.com/doc/refman/8.2/en/replication-features-timezone.html)
  - official: [MySQL server time zone support](https://dev.mysql.com/doc/mysql-g11n-excerpt/8.0/en/time-zone-support.html)
  - official: [Date and time functions](https://dev.mysql.com/doc/refman/en/date-and-time-functions.html)
  - official: [Time zone problems](https://dev.mysql.com/doc/mysql/en/timezone-problems.html)
  - community: [TIMESTAMP stores UTC while DATETIME does not](https://stackoverflow.com/questions/12951329/mysql-timestamp-field-type-automatically-converts-to-utc)
  - community: [Operational guidance on setting MySQL time zone explicitly](https://stackoverflow.com/questions/930900/how-do-i-set-the-time-zone-of-mysql)
  - community: [Session time_zone confusion and data interpretation caveats](https://stackoverflow.com/questions/2934258/how-do-i-get-the-current-time-zone-of-mysql)
  - community: [CONVERT_TZ named-zone pitfalls](https://stackoverflow.com/questions/19961183/mysql-convert-tz-function-does-not-work-for-timezone-codes)

## 110. Plugin lifecycle and disabled feature flags can quietly invalidate a migration plan

- Why it fails:
  - A schema or account can depend on a plugin or engine that exists on the source but is disabled, missing, removed, or no longer default on the target.
  - Auth plugins are the loudest example, but the same class exists for storage engines, full-text parser plugins, key management, audit plugins, and feature flags that gate plugin maturity or enablement.
  - A migration can therefore "succeed" structurally while leaving objects unusable or accounts unable to connect.
- Affected paths:
  - MySQL `8.0 -> 8.4 -> 9.x` auth transitions
  - MariaDB environments that rely on separately installed or lower-maturity plugins
  - cross-host moves where `plugin_dir`, package set, or engine enablement differs
- How to simulate:
  - Create accounts using `mysql_native_password`, then migrate into MySQL `8.4` with the plugin disabled by default and into `9.x` where it is removed.
  - Create tables or indexes that depend on optional engines or parser plugins, then restore to a target missing that capability.
  - Rehearse engine disablement with `NO_ENGINE_SUBSTITUTION` enabled and disabled.
- How to detect:
  - Inventory `mysql.user.plugin`, `SHOW PLUGINS`, `INFORMATION_SCHEMA.PLUGINS`, and `INFORMATION_SCHEMA.ENGINES`.
  - Record plugin maturity, package provenance, startup options, and whether an object becomes unusable if the plugin disappears.
  - Flag removed defaults such as `default_authentication_plugin` and version-specific auth behavior.
- Mitigation / fix:
  - Migrate accounts away from deprecated or removed auth plugins before cutover.
  - Fail fast if required plugins, engines, or parser capabilities are missing on the target.
  - Avoid relying on engine substitution as an implicit compatibility mechanism; require explicit confirmation instead.
- dbmigrate implication:
  - Plugin and engine inventory should remain a first-class compatibility report section, not an optional appendix.
  - Future matrix scenarios should include `mysql_native_password_removed`, `optional_engine_disabled`, and `plugin_backed_object_unusable`.
- Sources:
  - official: [MySQL native pluggable authentication](https://dev.mysql.com/doc/refman/8.1/en/native-pluggable-authentication.html)
  - official: [MySQL 8.4.0 release notes](https://dev.mysql.com/doc/relnotes/mysql/8.4/en/news-8-4-0.html)
  - official: [MySQL 9.0.0 release notes](https://dev.mysql.com/doc/relnotes/mysql/9.0/en/news-9-0-0.html)
  - official: [MySQL server plugins](https://dev.mysql.com/doc/refman/8.1/en/server-plugins.html)
  - official: [UNINSTALL PLUGIN statement](https://dev.mysql.com/doc/en/uninstall-plugin.html)
  - official: [MySQL storage engine setting and NO_ENGINE_SUBSTITUTION](https://dev.mysql.com/doc/refman/8.4/en/storage-engine-setting.html)
  - official: [MySQL INFORMATION_SCHEMA ENGINES table](https://dev.mysql.com/doc/refman/en/information-schema-engines-table.html)
  - official: [MariaDB plugin maturity](https://mariadb.com/docs/server/reference/plugins/list-of-plugins)
  - official: [MariaDB INSTALL PLUGIN](https://mariadb.com/docs/server/reference/sql-statements/administrative-sql-statements/plugin-sql-statements/install-plugin)
  - official: [MariaDB release criteria and plugin maturity](https://mariadb.com/kb/en/mariadb-release-criteria/)
  - community: [mysql_native_password disabled-by-default fallout in MySQL 8.4](https://stackoverflow.com/questions/79055662/using-chaching-sha2_password-but-getting-error-plugin-mysql-native-password-i)
  - community: [Client-side auth plugin loading breakage after version changes](https://stackoverflow.com/questions/49194719/authentication-plugin-caching-sha2-password-cannot-be-loaded)
  - community: [Real-world auth plugin breakage on newer MySQL client/server combinations](https://stackoverflow.com/questions/79285187/php-password-encryption-after-upgrade-from-mysql-5-7-to-mysql-8-4)

## 111. Replication parallelism does not rescue bad transaction shape

- Why it fails:
  - Parallel apply helps only when the source can expose safe concurrency and the workload actually contains parallelizable transactions.
  - Large transactions, DDL, foreign-key-related serialization points, missing primary or unique keys, and commit-order preservation can collapse an apparently parallel replica back to effectively serial execution.
  - Operators often keep tuning worker count when the real fix is to change chunking, transaction boundaries, or source-side dependency tracking.
- Affected paths:
  - incremental replication after baseline migration
  - catch-up windows after bulk backfill or large deletes
  - engine/version upgrades that change default multithreaded replica behavior
- How to simulate:
  - Compare a single massive delete or update against the same workload split into small commits.
  - Run DDL or FK-heavy updates during replication catch-up and measure worker utilization, lag, and state transitions such as `Waiting for preceding transaction to commit`.
  - Repeat with writeset-based dependency tracking where supported.
- How to detect:
  - Measure actual worker concurrency, not just configured `replica_parallel_workers`.
  - Track transaction size, presence of DDL, key coverage, FK usage, and commit-order settings alongside lag.
  - Watch for serialization symptoms in worker states and lag spikes that correlate with single huge transactions.
- Mitigation / fix:
  - Prefer smaller committed chunks over huge monolithic transactions for bulk changes.
  - Use online-schema-change patterns for large production DDL where feasible.
  - Tune parallel replication only after the workload shape and dependency rules are understood.
- dbmigrate implication:
  - Replication-readiness guidance should explicitly pair topology settings with transaction-shape guidance.
  - Future matrix scenarios should include `large_txn_vs_chunked_apply`, `ddl_forces_serial_apply`, and `fk_serialization_parallel_replication`.
- Sources:
  - official: [Replica server options and variables](https://dev.mysql.com/doc/mysql/8.0/en/replication-options-replica.html)
  - official: [Replication threads and monitoring worker threads](https://dev.mysql.com/doc/refman/8.4/en/replication-threads.html)
  - official: [Binary logging dependency tracking](https://dev.mysql.com/doc/mysql/8.0/en/replication-options-binary-log.html)
  - official: [MySQL Shell parallel replication applier configuration](https://dev.mysql.com/doc/mysql-shell/8.0/en/configuring-parallel-applier.html)
  - official: [MariaDB parallel replication](https://mariadb.com/docs/server/ha-and-performance/standard-replication/parallel-replication)
  - community: [Waiting for dependent transaction to commit on a lagging replica](https://forums.percona.com/t/lagging-replication-slave-sql-running-state-waiting-for-dependent-transaction-to-commit/23524)
  - community: [Multithreaded replication deadlock and commit-order stalls](https://forums.percona.com/t/multi-threaded-replication-deadlock/16337)
  - community: [Parallel replication only helps when workload really parallelizes](https://dba.stackexchange.com/questions/234209/parallel-replication-is-not-working-on-a-5-7-slave-from-a-5-7-master)
  - community: [Large transactions can dominate replica lag even without obvious saturation](https://dba.stackexchange.com/questions/315320/how-to-debug-a-huge-mysql-replication-lag)

## 112. Queue closure status

- Status:
  - As of `2026-03-06`, no additional high-priority research queue items were opened after the nineteenth pass.
- What remains:
  - Existing cloud-only, document-only, and lower-priority historical topics remain in this file for future dated updates if product scope changes or new field incidents justify them.
- Maintenance rule:
  - Reopen the live queue only when new evidence identifies a concrete migration or replication failure mode that is not already covered here with enough fidelity to simulate, detect, or document as unsupported.
