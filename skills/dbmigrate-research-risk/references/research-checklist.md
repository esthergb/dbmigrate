# Mandatory research checklist

Cover every domain below before implementation starts for the relevant phase.

## v1 domains (completed — preserve for reference)

1. MariaDB/MySQL feature incompatibilities
- JSON handling/storage and function differences
- syntax/DDL incompatibilities
- optimizer differences impacting deterministic verification
- sequence/system-versioned table behavior

2. Replication compatibility boundaries
- MySQL GTID vs MariaDB GTID incompatibility
- file/position fallback rules
- cross-engine replication limitations

3. Charset/collation pitfalls
- missing collations across versions
- illegal mix of collations
- upgrade collation behavior changes

4. Authentication and account migration
- MySQL 8+ auth plugin differences
- client/plugin compatibility breakages
- safe handling strategy for incompatible accounts

5. Reserved words and parser changes
- identifiers that become reserved across versions
- preflight scanner behavior and remediation hints

6. Binlog format limitations
- ROW vs STATEMENT/MIXED implications
- what must be refused or downgraded in capability

7. Operational failures at scale
- replication lag and backpressure
- large transactions and lock contention
- deadlocks/timeouts/retries and observability requirements

## v2 domains (research before implementation)

8. Stored procedures and functions
- DEFINER clause behavior differences across engines/versions
- SQL SECURITY INVOKER vs DEFINER implications during migration
- `sql_mode` dependency in routine bodies
- MariaDB-specific extensions (e.g. `RETURNS TABLE`, Oracle compatibility)
- MySQL-specific features (e.g. `LATERAL` derived tables in routines)
- Routine dependency ordering (routines calling other routines)

9. Triggers
- MariaDB `OR REPLACE` vs MySQL `CREATE TRIGGER` only
- Multiple triggers per timing/event (MySQL 8.0+ and MariaDB)
- Trigger ordering (`FOLLOWS`/`PRECEDES` clauses)
- DEFINER rewriting for triggers
- Cross-engine trigger syntax differences
- Impact on binlog: triggers fire on source but not on destination during replication

10. Events
- Event scheduler state (`@@event_scheduler`) differences
- DEFINER rewriting for events
- `ON COMPLETION [NOT] PRESERVE` behavior
- Timezone handling in event schedules
- Cross-engine event syntax compatibility

11. Trigger-based CDC
- Write amplification impact on source performance
- JSON serialization differences between MySQL and MariaDB (`JSON_OBJECT()`, `JSON_ARRAY()`)
- CDC log table locking and contention under concurrent writes
- Trigger naming collision avoidance strategies
- Cleanup/teardown safety for long-running migrations
- Interaction with existing user triggers on same tables

12. GTID replication
- MySQL GTID format (`server_uuid:transaction_id`) vs MariaDB (`domain_id-server_id-sequence`)
- `@@gtid_mode` requirements and verification
- GTID set persistence and resume semantics
- Cross-engine GTID incompatibility (cannot cross engines)
- Purged GTID sets and missing transaction detection

13. User and grant migration
- `mysql.user` table schema differences across versions
- `SHOW GRANTS` output format variations
- Authentication plugin compatibility matrix (mysql_native_password, caching_sha2_password, ed25519, etc.)
- Role support differences (MySQL 8.0+ vs MariaDB 10.0.5+)
- System account identification and exclusion rules
- Password hash format differences across auth plugins

14. Concurrent data copy
- Connection pool sizing vs server `max_connections`
- Consistent snapshot isolation across multiple connections
- Checkpoint atomicity with concurrent writers
- Memory pressure from parallel chunk buffers
- Deadlock potential during concurrent inserts on destination
