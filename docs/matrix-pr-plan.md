# Matrix PR Plan

Last reviewed: 2026-03-06

This document converts the local simulation roadmap in [migration-replication-conflict-history.md](./migration-replication-conflict-history.md) into a phase-by-phase PR plan. It is intentionally documentation-only: no code is changed here.

## Planning rules

- One focused PR per phase.
- Each phase adds datasets, configs, scripts, probes, and report updates only for its own scope.
- Prefer earlier phases that improve fail-fast prechecks and broad operator signal.
- Keep CI minimal; full matrix and failure-report evidence stay local unless promoted later.

## Phase 1: Collation incompatibility probes

- Scope:
  - `unsupported_collations_mysql_to_mariadb`
  - `unsupported_collations_mariadb_to_mysql`
- Deliverables:
  - new dataset fixtures with `utf8mb4_0900_ai_ci` and `utf8mb4_uca1400_ai_ci`
  - scenario configs covering both directions
  - report wording for `server-side unsupported` versus `client-side unsupported`
- Exit criteria:
  - scenarios fail deterministically with clear incompatibility reports
  - documentation points to exact collation names and recommended mappings

## Phase 2: Auth/plugin matrix and client-compat guardrails

- Scope:
  - `auth_plugin_matrix`
- Deliverables:
  - account fixtures for `caching_sha2_password`, `mysql_native_password`, and MariaDB socket-style defaults
  - operator-facing compatibility report expectations
  - docs for representative client-library smoke checks
- Exit criteria:
  - unsupported plugin/client combinations are surfaced before cutover
  - report separates business accounts from system accounts cleanly

## Phase 3: Charset alias and config/bootstrap drift

- Scope:
  - `utf8mb3_alias_upgrade`
  - `stale_config_upgrade`
  - `obsolete_sql_bootstrap`
- Deliverables:
  - fixtures for `utf8`/`utf8mb3`
  - startup-config examples with removed variables
  - bootstrap SQL samples that use removed syntax or SQL modes
- Exit criteria:
  - upgrade-precheck findings clearly separate schema drift from config/init drift
  - docs explain which issues are simulator-backed versus document-only

## Phase 4: Dump/import fallback correctness

- Scope:
  - `dump_tool_skew`
  - `dump_packet_limit`
  - `dump_binary_mode_corruption`
  - `dump_encoding_drift`
- Deliverables:
  - dump fixtures produced by MySQL 8.4 and 9.6 clients
  - repro scripts for packet-limit and binary-mode failures
  - report annotations for wrong-client and wrong-encoding cases
- Exit criteria:
  - fallback dump/import failures are reproducible locally
  - docs show exact flags that avoid the reproduced failures

## Phase 5: Generated columns and PK-ordering dump traps

- Scope:
  - `generated_columns_cross_engine`
  - `sql_require_primary_key_dump`
- Deliverables:
  - generated-column fixtures for MySQL and MariaDB
  - dump shapes where PK is added later with `ALTER TABLE`
  - recommendations for dump rewrite or engine-native tooling
- Exit criteria:
  - generated-column and PK-order failures are distinct in reporting
  - docs no longer treat all dump failures as generic import errors

## Phase 6: Schema object and dependency integrity

- Scope:
  - `definer_orphan_objects`
  - `cross_db_partial_scope`
  - `fk_order_replay`
  - `event_scheduler_drift`
  - `timezone_table_drift`
  - `persisted_config_drift`
- Deliverables:
  - fixtures with missing definers, cross-db references, FK ordering issues, events, and timezone-sensitive logic
  - report sections for omitted dependencies and runtime-risk objects
- Exit criteria:
  - partial-scope and object-dependency failures are explicit before cutover
  - docs explain why "database selected" does not equal "dependency complete"

## Phase 7: Replication retention and topology identity

- Scope:
  - `binlog_retention_expiry`
  - `duplicate_replica_identity`
- Deliverables:
  - retention-expiry scenario
  - cloned-identity scenario using duplicated `server_id` / `server_uuid`
  - reporting for topology identity and retention windows
- Exit criteria:
  - replica resume and topology-integrity failures are deterministic
  - docs tell operators when rebuild is required instead of resume

## Phase 8: Recovery false positives and restart safety

- Scope:
  - `skip_counter_false_recovery`
  - `relay_metadata_restart`
  - `prepared_xa_blocker`
  - `gipk_divergence`
- Deliverables:
  - row-drift recovery scenario using skip counter
  - restart/crash resume scenario
  - XA blocker scenario
  - GIPK divergence documentation and fixture support where feasible
- Exit criteria:
  - reports separate "stream restarted" from "state is trustworthy"
  - docs state when rebuild/resync is the only credible recovery path

## Phase 9: Metadata lock and partition concurrency hazards

- Scope:
  - `metadata_lock_window`
  - `partition_ddl_blocking`
- Deliverables:
  - long-transaction plus DDL fixtures
  - partition maintenance concurrency scenarios
  - runbook notes for low-timeout schema operations
- Exit criteria:
  - lock-queue blast radius is measurable in local rehearsal
  - docs explain why pending DDL can block later ordinary traffic

## Phase 10: Online schema change hazards

- Scope:
  - `online_ddl_cutover_lock`
  - `online_ddl_duplicate_loss`
  - `online_ddl_trigger_conflict`
- Deliverables:
  - `pt-online-schema-change` and `gh-ost` reproductions where practical
  - duplicate-key and trigger/FK hazard fixtures
  - docs that treat online DDL tools as separate risk classes
- Exit criteria:
  - cut-over and trigger risks are reproduced or explicitly marked unsupported
  - docs stop framing online DDL as a generic escape hatch

## Phase 11: Upgrade-only edge cases

- Scope:
  - `invisible_columns_downgrade`
  - `spatial_index_upgrade_gate`
- Deliverables:
  - version-gated upgrade/downgrade fixtures
  - docs for invisible-column visibility drift and spatial-index exceptions
- Exit criteria:
  - upgrade-only exceptions are isolated from generic schema migration failures
  - remaining version-specific gaps are documented as explicit evidence requests

## Phase 12: Verification representation traps

- Scope:
  - verification false positives
  - checksum/hash mismatch traps
  - approximate numeric and timezone-sensitive verify behavior
- Deliverables:
  - fixtures for `FLOAT`, `DOUBLE`, `TIMESTAMP`, JSON normalization, and collation-sensitive hashing
  - report wording for `possible_false_positive_due_to_representation`
- Exit criteria:
  - verify output distinguishes likely real drift from representation noise

## Phase 13: Data-type edge-case fixtures

- Scope:
  - `dump_packet_limit`
  - `dump_binary_mode_corruption`
  - `dump_encoding_drift`
  - connector metadata traps around `TINYINT(1)`, `BIT`, JSON, and Unicode
- Deliverables:
  - large payload fixtures
  - Unicode-heavy fixtures
  - documentation on client and connector interpretation risks
- Exit criteria:
  - data-type edge cases are reproducible and reported separately from generic import failure

## Phase 14: Backup-tool compatibility documentation track

- Scope:
  - `backup_tool_compatibility`
- Deliverables:
  - explicit docs boundary between logical migration and physical backup workflows
  - vendor matrix notes for `mariadb-backup`, `mariabackup`, XtraBackup, and MySQL Enterprise Backup
- Exit criteria:
  - operators cannot confuse dbmigrate support with physical backup portability

## Phase 15: TLS and client transport validation track

- Scope:
  - `tls_ssl_transport_breakage`
- Deliverables:
  - client TLS smoke-test checklist
  - transport capability notes by representative driver family
- Exit criteria:
  - cutover runbooks include client transport validation, not only SQL connectivity

## Phase 16: Advanced topology and parser-drift documentation track

- Scope:
  - `routine_view_parser_drift`
  - `multi_source_filtering_drift`
- Deliverables:
  - stored-object parser-drift checklist
  - documentation-first guidance for multi-source and filtered replication
- Exit criteria:
  - stored objects and advanced topologies are explicitly separated from the core supported path

## Phase 17: DDL algorithm and lock-clause evidence

- Scope:
  - `ddl_algorithm_and_lock_clause_drift`
- Deliverables:
  - documentation and scenario design for `ALGORITHM=INSTANT|INPLACE|COPY` and `LOCK` clause support boundaries
  - examples showing metadata-lock waits despite "online" clauses
- Exit criteria:
  - runbooks stop treating online DDL clauses as guarantees

## Phase 18: Compression and storage-format downgrade documentation track

- Scope:
  - `compression_and_page_format_drift`
- Deliverables:
  - storage-format inventory checklist
  - docs describing compressed/page-compressed downgrade and restore risks
- Exit criteria:
  - compression features are documented as separate storage-risk attributes

## Phase 19: Parallel applier diagnostics track

- Scope:
  - `replication_applier_parallelism_edge_cases`
- Deliverables:
  - worker-level diagnostic checklist using `performance_schema`
  - report design notes for worker failures and commit-order assumptions
- Exit criteria:
  - replication diagnostics plan includes worker-level evidence, not only top-level status

## Phase 20: Connector and ORM metadata validation track

- Scope:
  - `connector_metadata_and_orm_drift`
- Deliverables:
  - representative connector/ORM smoke checklist
  - documentation for boolean/bit/JSON metadata interpretation traps
- Exit criteria:
  - application metadata validation is treated as a separate cutover gate

## Phase 21: Bulk-load privilege and file-path validation track

- Scope:
  - `load_data_infile_and_secure_file_priv`
- Deliverables:
  - docs for `LOAD DATA INFILE` versus `LOAD DATA LOCAL INFILE`
  - rehearsal checklist for `secure_file_priv`, `local_infile`, and tool-specific behavior
- Exit criteria:
  - fallback bulk-load workflows have explicit privilege and path prechecks

## Phase 22: Online DDL clause semantics track

- Scope:
  - `ddl_algorithm_and_lock_clause_drift`
- Deliverables:
  - clause-by-clause support notes for `ALGORITHM` and `LOCK`
  - rehearsal guidance that distinguishes requested online behavior from guaranteed online behavior
- Exit criteria:
  - online-DDL runbooks explicitly model clause fallback and metadata-lock risk

## Phase 23: Compression and page-format risk track

- Scope:
  - `compression_and_page_format_drift`
- Deliverables:
  - storage-format checklist for downgrade and physical restore planning
  - docs for compressed/page-compressed table handling before downgrade
- Exit criteria:
  - compression features are treated as upgrade/downgrade blockers when unsupported

## Phase 24: Parallel replication worker diagnostics track

- Scope:
  - `replication_applier_parallelism_edge_cases`
- Deliverables:
  - worker-level troubleshooting guidance
  - reporting notes for `replication_applier_status_by_worker` and commit-order assumptions
- Exit criteria:
  - replica recovery guidance includes worker-state analysis, not only coordinator state

## Phase 25: ORM and connector metadata behavior track

- Scope:
  - `connector_metadata_and_orm_drift`
- Deliverables:
  - representative ORM/connector metadata validation checklist
  - docs for `TINYINT(1)`, `BIT`, JSON, and MariaDB/MySQL driver-family interpretation
- Exit criteria:
  - application metadata regression risk is documented as a separate validation gate

## Phase 26: Bulk-load security and tool behavior track

- Scope:
  - `load_data_infile_and_secure_file_priv`
- Deliverables:
  - docs for `LOAD DATA INFILE` versus `LOCAL INFILE`
  - checklist for `secure_file_priv`, `local_infile`, and tool-specific import behavior
- Exit criteria:
  - bulk-load fallback workflows are documented with explicit security and path assumptions

## Phase 27: External-table and plugin dependency track

- Scope:
  - `federated_fdw_and_external_table_edges`
- Deliverables:
  - unsupported-feature notes for `FEDERATED`, `CONNECT`, and similar external-table dependencies
  - runbook guidance for remote endpoint and plugin inventory
- Exit criteria:
  - plugin-dependent tables are clearly separated from ordinary logical schema

## Phase 28: Generated-column and expression-default drift track

- Scope:
  - `generated_column_and_expression_default_drift`
- Deliverables:
  - transform/precheck design notes for generated columns and expression defaults
  - documentation for dump/import failure patterns around generated values
- Exit criteria:
  - generated-column and expression-default drift are documented as dedicated compatibility classes

## Phase 29: Effective privilege and role-activation track

- Scope:
  - `security_definer_and_role_activation_edges`
- Deliverables:
  - checklist for `SQL SECURITY`, definers, roles, and default-role state
  - application-identity execution smoke-test guidance
- Exit criteria:
  - privilege migration guidance includes effective runtime authorization, not only grant text

## Phase 30: Long-transaction and recovery window track

- Scope:
  - `redo_undo_log_capacity_and_long_transaction_recovery`
- Deliverables:
  - runtime-risk checklist for redo capacity, undo growth, purge lag, and crash recovery
  - scenario design notes for large-transaction restart drills
- Exit criteria:
  - migration runbooks treat long transaction recovery as a first-class operational risk

## Phase 31: Tool-divergence validation track

- Scope:
  - `client_shell_gui_tooling_divergence`
- Deliverables:
  - comparative checklist for `mysql`, `mysqlsh`, Workbench, and representative drivers
  - docs for tool-specific defaults that affect import, SSL, and metadata inspection
- Exit criteria:
  - operator workflows name the exact tool used for each critical action and do not rely on generic client equivalence

## Phase 32: DDL rebuild-cost modeling track

- Scope:
  - `ddl_copy_algorithm_and_rebuild_costs`
- Deliverables:
  - rebuild-cost checklist for large alters
  - docs distinguishing clause support from actual temp-space/runtime cost
- Exit criteria:
  - rebuild-heavy DDL is treated as an operational-risk class, not a generic supported change

## Phase 33: Charset handshake and connection negotiation track

- Scope:
  - `charset_connection_handshake_drift`
- Deliverables:
  - handshake-focused client compatibility checklist
  - docs separating connection-time charset failure from query-time collation failure
- Exit criteria:
  - connection negotiation is modeled as its own compatibility gate

## Phase 34: Trigger-order and behavioral verification track

- Scope:
  - `trigger_order_and_multiple_trigger_semantics`
- Deliverables:
  - trigger-chain inventory checklist
  - docs for behavioral smoke testing of trigger side effects
- Exit criteria:
  - trigger-heavy schemas are no longer treated as table-only validation cases

## Phase 35: Canonical checksum and row-serialization track

- Scope:
  - `checksum_tooling_and_row_canonicalization`
- Deliverables:
  - canonical hashing guidance
  - docs for why naive checksums produce false positives
- Exit criteria:
  - verification guidance clearly defines deterministic ordering and serialization requirements

## Phase 36: Filtered-scope object-dependency track

- Scope:
  - `replication_filtering_with_views_and_routines`
- Deliverables:
  - docs for filtered replication plus stored-object dependency holes
  - checklist for cross-db references in views and routines
- Exit criteria:
  - filtered-scope runs explicitly report object dependency gaps before cutover

## Phase 37: Optimizer-statistics and plan-regression track

- Scope:
  - `histogram_statistics_and_optimizer_drift`
- Deliverables:
  - post-cutover statistics refresh checklist
  - docs distinguishing performance regressions from data drift
- Exit criteria:
  - runbooks explicitly handle plan-regression triage after successful verify

## Phase 38: Specialized index and GIS/FULLTEXT track

- Scope:
  - `spatial_gis_and_fulltext_edge_cases`
- Deliverables:
  - feature-specific validation checklist for spatial and FULLTEXT workloads
  - docs for specialized index rebuild and query-level smoke tests
- Exit criteria:
  - specialized index classes are not treated as generic table validation

## Phase 39: Event replay and time-zone behavior track

- Scope:
  - `event_scheduler_and_time_zone_replay_edges`
- Deliverables:
  - scheduler-state and time-zone validation checklist
  - post-cutover event-execution smoke-test guidance
- Exit criteria:
  - event-heavy workloads have explicit behavioral validation after restore

## Phase 40: Account usability and password-export track

- Scope:
  - `password_hash_format_and_account_export_edges`
- Deliverables:
  - docs for account export pitfalls beyond plugin selection
  - authentication smoke-test checklist for application identities
- Exit criteria:
  - account migration signoff requires usable authentication, not only syntactic account recreation

## Phase 41: Proxy and router path validation track

- Scope:
  - `proxy_pooler_and_connection_router_behavior`
- Deliverables:
  - runbook checklist for rehearsing through the production connection path
  - docs for proxy-induced routing and failover differences
- Exit criteria:
  - direct-connect rehearsal is explicitly distinguished from proxied production behavior

## Phase 42: Optimizer-hint and trace portability track

- Scope:
  - `optimizer_trace_and_hint_drift`
- Deliverables:
  - checklist for optimizer hints, `SET_VAR`, and optimizer-trace-aware tuning
  - docs for hint portability and stale-plan risk
- Exit criteria:
  - advanced hint-driven workloads are treated as a dedicated upgrade validation class

## Phase 43: Deep GIS/SRS compatibility track

- Scope:
  - `spatial_reference_system_and_geometry_encoding_edges`
- Deliverables:
  - SRS-aware GIS validation checklist
  - docs for geometry compatibility beyond spatial indexes
- Exit criteria:
  - GIS-heavy workloads are not signed off on schema alone

## Phase 44: Event failure runbook track

- Scope:
  - `event_scheduler_failure_runbooks`
- Deliverables:
  - operator decision tree for event failures
  - post-cutover event execution validation checklist
- Exit criteria:
  - event-heavy workloads have explicit failure handling guidance

## Phase 45: Account policy and usability track

- Scope:
  - `account_lock_password_policy_and_expiry_edges`
- Deliverables:
  - account usability checklist covering lock, expiry, and policy state
  - login smoke-test guidance for application identities
- Exit criteria:
  - account migration acceptance requires usable accounts, not only recreated accounts

## Phase 46: Proxy failover and read/write split track

- Scope:
  - `proxy_failover_consistency_and_read_write_split_edges`
- Deliverables:
  - proxy-specific cutover and failover validation checklist
  - docs for read/write routing assumptions during topology change
- Exit criteria:
  - proxied production paths are validated independently from direct DB access

## Phase 47: GIPK runtime-behavior track

- Scope:
  - `generated_invisible_primary_key_runtime_edges`
- Deliverables:
  - runtime and failover notes for generated invisible primary keys
  - docs distinguishing explicit PKs from generated hidden keys
- Exit criteria:
  - GIPK behavior is documented beyond dump and downgrade mechanics

## Phase 48: Stored-object partial-scope interaction track

- Scope:
  - `view_definer_sql_security_and_partial_scope_interactions`
- Deliverables:
  - partial-scope checklist for views, routines, definers, and `SQL SECURITY`
  - docs for cross-db stored-object dependency reporting
- Exit criteria:
  - stored-object scope holes are called out before cutover, not after runtime failures

## Phase 49: Chunking and autocommit behavior track

- Scope:
  - `large_transaction_chunking_and_autocommit_semantics`
- Deliverables:
  - transaction-shape checklist for chunking and autocommit
  - docs linking chunk boundaries to lag, restart safety, and recovery cost
- Exit criteria:
  - large migrations are planned around transaction shape, not only row volume

## Phase 50: Exporter and monitoring-agent compatibility track

- Scope:
  - `monitoring_agent_and_exporter_compatibility`
- Deliverables:
  - monitoring-agent validation checklist
  - docs for exporter/client drift after engine or version changes
- Exit criteria:
  - monitoring continuity is treated as an explicit cutover acceptance gate

## Phase 51: Non-InnoDB engine behavior track

- Scope:
  - `engine_specific_non_innodb_table_behavior`
- Deliverables:
  - engine inventory checklist with runbook notes per engine family
  - docs for non-InnoDB restore, locking, and durability surprises
- Exit criteria:
  - non-InnoDB objects are surfaced as dedicated risk classes before migration

## Phase 52: Stored-object parser and SQL-mode behavior track

- Scope:
  - `routine_parser_reserved_word_and_sql_mode_edges`
- Deliverables:
  - stored-object parser checklist with SQL-mode-sensitive cases
  - invocation smoke-test guidance for routine and view behavior
- Exit criteria:
  - stored-object parser drift is handled separately from base table DDL drift

## Phase 53: Temporary table and session-state behavior track

- Scope:
  - `temporary_table_and_session_state_edges`
- Deliverables:
  - runbook notes for temp tables, user variables, and session-state-sensitive jobs
  - smoke-test checklist for pooled versus fresh connections
- Exit criteria:
  - session-state-heavy workflows are explicitly validated post-cutover

## Phase 54: GTID reseed and repair-safety track

- Scope:
  - `gtid_set_surgery_and_reseed_edges`
- Deliverables:
  - conservative rebuild/reseed guidance
  - docs for unsafe GTID surgery patterns and safer alternatives
- Exit criteria:
  - GTID recovery guidance explicitly favors verified rebuild paths over ad hoc state edits

## Phase 55: Filesystem and path-semantics track

- Scope:
  - `filesystem_case_symlink_and_path_semantics`
- Deliverables:
  - environment compatibility checklist for path, symlink, and case behavior
  - docs linking filesystem semantics to import/export and identifier behavior
- Exit criteria:
  - platform path semantics are treated as migration inputs, not deployment trivia

## Phase 56: Replication credential rotation track

- Scope:
  - `replication_privilege_and_channel_user_rotation`
- Deliverables:
  - replication-user lifecycle checklist
  - channel credential rotation validation guidance
- Exit criteria:
  - replication readiness includes user rotation and privilege checks, not only initial setup

## Phase 57: Metadata-lock observability and runbook track

- Scope:
  - `ddl_metadata_lock_observability_runbooks`
- Deliverables:
  - scenario notes for metadata-lock queue amplification
  - operator runbook guidance for blocker identification, safe abort, and observability capture
- Exit criteria:
  - metadata-lock incidents are diagnosed as a first-class migration failure mode rather than a vague locking problem

## Phase 58: Backup restore-rehearsal assurance track

- Scope:
  - `backup_restore_validation_gaps`
- Deliverables:
  - restore-rehearsal checklist distinguishing backup completion, validation, and restore usability
  - docs for physical-backup prepare requirements and version/tool mismatch hazards
- Exit criteria:
  - rollback confidence requires restore evidence, not just successful backup jobs

## Phase 59: Time-zone and NOW-semantics track

- Scope:
  - `session_timezone_and_now_function_behavior`
- Deliverables:
  - compatibility checklist for `time_zone`, `system_time_zone`, `TIMESTAMP`, `DATETIME`, and time-sensitive functions
  - smoke-test guidance for DST and session-init behavior
- Exit criteria:
  - time-function behavior is validated explicitly during cutover planning

## Phase 60: Plugin lifecycle and disabled-feature track

- Scope:
  - `plugin_lifecycle_and_disabled_feature_flags`
- Deliverables:
  - inventory checklist for auth plugins, storage engines, parser plugins, and plugin-backed objects
  - docs for removed defaults, disabled-by-default features, and object usability fallout
- Exit criteria:
  - plugin and engine dependencies fail fast instead of surfacing after migration

## Phase 61: Replication parallelism versus transaction-shape track

- Scope:
  - `replication_parallelism_vs_chunking_interactions`
- Deliverables:
  - scenario notes comparing large transactions with chunked workloads under replication
  - docs tying worker settings to DDL, FK serialization, key coverage, and commit-order behavior
- Exit criteria:
  - replication tuning guidance is grounded in transaction shape, not worker count alone

## Phase 62: Invisible-column and GIPK downgrade-evidence track

- Scope:
  - `invisible_columns_downgrade`
  - `gipk_divergence`
- Deliverables:
  - fixtures and rehearsal coverage for invisible columns, invisible-index visibility drift, and generated invisible primary key dump behavior
  - downgrade and cross-engine evidence comparing `dump with GIPK included`, `dump with GIPK skipped`, and `older target makes invisible visible`
  - product-side reporting or precheck output that distinguishes explicit primary keys from generated invisible ones where feasible
- Exit criteria:
  - invisible-column and GIPK downgrade behavior is backed by local matrix evidence instead of remaining `UNCONFIRMED`

## Phase 63: Collation client-compatibility and handshake-risk track

- Scope:
  - `unsupported_collations_mysql_to_mariadb`
  - `unsupported_collations_mariadb_to_mysql`
  - client-side collation and handshake incompatibility where the server accepts the schema but the application stack does not
- Deliverables:
  - precheck/report output that distinguishes server-side unsupported collations from client-side compatibility risk
  - fixtures and rehearsal coverage for `utf8mb4_0900_ai_ci` and `utf8mb4_uca1400_ai_ci`
  - operator guidance that separates storage-collation drift from connection-handshake or client-library drift
- Exit criteria:
  - collation compatibility is surfaced as a first-class planning result with explicit server-versus-client risk framing instead of a generic charset warning

## Phase 64: Verification canonicalization and false-positive control track

- Scope:
  - `verification_false_positives`
  - `checksum_hash_mismatch_row_canonicalization`
  - data-type edge cases where representation drift looks like data drift
- Deliverables:
  - verify/report behavior that distinguishes real row drift from canonicalization-sensitive differences
  - fixtures and rehearsal coverage for ordering, time-zone, JSON, floating-point, and collation-sensitive comparisons
  - operator guidance for when to trust `count`, `hash`, `sample`, and `full-hash` results
- Exit criteria:
  - verification output is explicit about canonicalization assumptions and reduces high-noise false positives in reproducible scenarios

## Follow-on cloud track

- Keep these out of the local PR sequence unless local approximations become genuinely useful:
  - `managed_target_scope_gap`
  - `managed_cdc_rename_block`
  - `managed_destination_not_empty`
  - `failover_durability_gap`
  - `azure_gtid_transition`
  - `dns_failover_stale_pool`

## Evidence expectations per PR

- Updated report section in `docs/migration-replication-conflict-history.md`
- New or revised datasets/configs/scripts only for the phase scope
- Local execution notes with exact scenario names and outcomes
- Clear statement of what remains `UNCONFIRMED`
