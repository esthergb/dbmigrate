# Version Compatibility Research (MySQL 8.0/8.4, MariaDB 10.6/11.0/12.0)

This document captures official compatibility differences relevant to the current dbmigrate matrix and maps each difference to executable compatibility probes.

## Scope

- MySQL: `8.0.x`, `8.4.x`
- MariaDB: `10.6.x`, `11.0.x`, `12.0.x`
- Paths: same-engine and cross-engine (MariaDB <-> MySQL)

## Official Sources Reviewed

- MySQL 8.4 release notes: <https://dev.mysql.com/doc/relnotes/mysql/8.4/en/news-8-4-0.html>
- MySQL 8.4 downgrade behavior and restrictions: <https://dev.mysql.com/doc/refman/8.4/en/downgrading.html>
- MySQL SQL mode and zero-date behavior: <https://dev.mysql.com/doc/refman/8.4/en/sql-mode.html>
- MySQL date/time type ranges and rules: <https://dev.mysql.com/doc/refman/8.4/en/datetime.html>
- MariaDB 11.0 vs MySQL 8.0 differences: <https://mariadb.com/kb/en/incompatibilities-and-feature-differences-between-mariadb-11-0-and-mysql-8/>
- MariaDB 10.6 vs MySQL 8.0 differences: <https://mariadb.com/kb/en/incompatibilities-and-feature-differences-between-mariadb-10-6-and-mysql-8/>
- MariaDB 12.0 changes and MySQL compatibility improvements: <https://mariadb.com/docs/release-notes/community-server/changelogs/changelogs-mariadb-12-0-series/what-is-mariadb-120>
- MariaDB 11.8 changes (defaults that affect 12.x line): <https://mariadb.com/docs/release-notes/community-server/changelogs/changelogs-mariadb-11-8-series/what-is-mariadb-118>
- MariaDB major upgrade incompatibilities example (10.11 -> 11.0): <https://mariadb.com/kb/en/upgrading-from-mariadb-10-11-to-mariadb-11-0/>
- MariaDB SQL mode reference: <https://mariadb.com/kb/en/sql-mode/>
- MariaDB TIMESTAMP limits and behavior: <https://mariadb.com/kb/en/timestamp/>

## Key Differences and Probe Coverage

| Difference Area | Official Evidence | Affected in Current Matrix | Probe ID |
|---|---|---|---|
| `SET PERSIST` support | MariaDB docs explicitly list it as unsupported vs MySQL | Cross-engine and same-engine behavior split | `set_persist` |
| JSON `->` / `->>` operators | MariaDB docs list unsupported operators vs MySQL | Cross-engine SQL compatibility | `json_arrow_operators` |
| `LATERAL` derived tables | MariaDB docs list unsupported syntax vs MySQL | Cross-engine SQL compatibility | `lateral_derived_table` |
| `max_execution_time` vs `max_statement_time` knobs | MariaDB docs note MySQL-specific timeout hint/variable differences | Cross-engine SQL/session behavior | `max_execution_time_variable`, `max_statement_time_variable` |
| `SET SESSION AUTHORIZATION` support in MariaDB 12 | MariaDB 12.0 release notes add this capability | Same-engine MariaDB line differences and cross-engine behavior | `set_session_authorization` |
| Restricting non-standard FK references in MySQL 8.4 | MySQL 8.4 docs describe `restrict_fk_on_non_standard_key` behavior | MySQL 8.0 vs 8.4 and cross-engine DDL behavior | `restrict_fk_on_non_standard_key_variable`, `nonstandard_fk_reference` |
| `default_authentication_plugin` removal in MySQL 8.4 | MySQL 8.4 release notes remove/replace related behavior | MySQL 8.0 vs 8.4 | `default_authentication_plugin_variable` |
| `mysql_native_password` default availability changes | MySQL 8.4 release notes disable plugin by default | MySQL 8.0 vs 8.4, migration user/grant compatibility checks | `mysql_native_password_plugin` |
| Default server charset/collation shifts in MariaDB newer lines | MariaDB 11.8 notes default changes (`utf8mb4` + UCA1400 collation family) | MariaDB line differences (10.6/11.0/12.0) and cross-engine collation behavior | `server_character_set`, `server_collation` |
| Session SQL mode defaults diverge across engines | MySQL and MariaDB SQL mode references | Reproducing strict-mode incompatibility findings | `session_sql_mode` |
| Legacy zero-date / zero-in-date defaults under strict SQL mode | MySQL SQL mode docs (`NO_ZERO_DATE`, `NO_ZERO_IN_DATE`) + MariaDB SQL mode defaults differ in practice | Real-world legacy dumps and cross-engine migrations | `zero_datetime_default_strict`, `zero_timestamp_default_strict`, `zero_date_default_strict`, `zero_in_date_default_strict` |
| Invalid temporal defaults (calendar-invalid and out-of-range timestamp) | MySQL/MariaDB date-time type rules and timestamp range constraints | Same `ERROR 1067` family hard-fail checks relevant to precheck design | `invalid_calendar_date_default`, `invalid_calendar_datetime_default`, `timestamp_out_of_range_default` |

## How Tests Simulate These Differences

The matrix runner now executes `scripts/run-compat-probes.sh` for both source and destination services before each migration scenario. The probe runner:

- executes SQL/DDL statements that intentionally behave differently across versions/engines
- records probe-level pass/fail and raw SQL stdout/stderr
- stores artifacts in scenario state:
  - `state/<scenario>/compat-probes-source.json`
  - `state/<scenario>/compat-probes-dest.json`

These probe artifacts complement migration/verify results and make version-specific behavior explicit in each scenario run.

## Notes

- The MariaDB "differences vs MySQL" pages are marked as unmaintained in some versions but still provide explicit incompatibility callouts used here.
- Probe failures are expected for some version/engine combinations and are treated as compatibility signals, not harness failures.
- This probe suite targets SQL/DDL/session-level behavior differences that are reproducible in containerized matrix tests.
