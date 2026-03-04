# Mandatory research checklist

Cover every domain below before implementation starts.

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
