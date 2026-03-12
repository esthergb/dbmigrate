# dbmigrate Code Review

## 1. Executive Summary

* **Solid Architecture & Foundation**: The project has a strong single-binary approach utilizing `go-mysql` for binlog replication, with excellent scaffolding for baseline migration, incremental replication, and data verification.
* **Excellent Verification Mechanisms**: The canonicalization and hashing strategies in the `verify` command (`internal/verify/data/verify.go`) correctly handle representations risks (timezones, collation, approximate numerics).
* **Missing Checkpoint Safety**: Baseline migration (`internal/data/copy.go`) uses `LIMIT ? OFFSET ?` without primary key ordering. This risks missing/duplicating rows if data mutates during the baseline copy.
* **Missing Checkpoint Atomicity**: Binlog replication (`internal/replicate/binlog/run.go`) commits destination transactions separately from saving the checkpoint to JSON, which can lead to idempotency issues and double-replay on crash.
* **Unsafe Default DDL Handling**: While replication supports `ignore/apply/warn`, DDL `apply` blindly executes string replacement which is risky.
* **Missing Snapshot Consistency**: Baseline schema and data migration does not use `--single-transaction` or `START TRANSACTION WITH CONSISTENT SNAPSHOT`, leading to inconsistent baseline copies on active systems.
* **Memory Bounding Issues**: Full table row hashes in `VerifyFullHash` buffer all normalized row hashes in memory before final sha256. This will OOM on large tables (hundreds of GB).
* **CI/CD Missing Artifact Checks**: The GitHub Action is basic. It misses build artifact generation, SBOM, and explicit dependency checks (`govulncheck` is run but no artifact is saved).
* **Good Testing Harness**: The Docker Compose matrix covers MariaDB 10/11 and MySQL 8.0/8.4 well, but lacks explicit deterministic chaos testing.
* **Clean Code Structure**: Code is well modularized, standard CLI flag parsing is cleanly decoupled, and minimal external dependencies are used.

## 2. High-Risk Findings

* **Unsafe Pagination for Baseline Migration**
  * **Impact**: Critical. Data corruption (missing or duplicated rows) during baseline data copy if the source table is actively being written to.
  * **Likelihood**: High on production systems with active traffic.
  * **Evidence**: `@/Volumes/UserData/Works/dbmigrate/internal/data/copy.go:414-419` `SELECT %s FROM %s.%s LIMIT ? OFFSET ?` (no `ORDER BY`).
  * **Fix Strategy**: Change pagination to keyset pagination (e.g., `WHERE pk > ? ORDER BY pk LIMIT ?`).

* **Inconsistent Baseline Snapshot**
  * **Impact**: Critical. The baseline data copy runs outside of a globally consistent snapshot, meaning cross-table foreign keys and replication handoff will be fundamentally broken on active systems.
  * **Likelihood**: High on production systems.
  * **Evidence**: `@/Volumes/UserData/Works/dbmigrate/internal/data/copy.go:130` Data is fetched query-by-query without a long-running transaction with `START TRANSACTION WITH CONSISTENT SNAPSHOT`.
  * **Fix Strategy**: Pin the source connection, execute `START TRANSACTION WITH CONSISTENT SNAPSHOT`, record the binlog coordinates, and execute all `SELECT` queries within that pinned transaction.

* **Replication Checkpoint Not Atomic with Data**
  * **Impact**: High. Replaying transactions after a crash could lead to duplicate key errors if the policy is `fail`.
  * **Likelihood**: Medium (requires crash exactly between DB commit and file write).
  * **Evidence**: `@/Volumes/UserData/Works/dbmigrate/internal/replicate/binlog/run.go:626-640` `tx.Commit()` happens, followed later by `@/Volumes/UserData/Works/dbmigrate/internal/replicate/binlog/run.go:180-186` `state.SaveReplicationCheckpoint()`.
  * **Fix Strategy**: To achieve true exactly-once semantics, write the checkpoint position into a metadata table on the destination database inside the *same* transaction (`tx`) as the data changes.

* **OOM Risk on Verification Hash**
  * **Impact**: High. The process will be killed by the OS (OOM) when verifying tables with hundreds of GBs of data.
  * **Likelihood**: High for production datasets.
  * **Evidence**: `@/Volumes/UserData/Works/dbmigrate/internal/verify/data/verify.go:490` `rowHashes = append(rowHashes, hex.EncodeToString(rowSum[:]))` appends every single row's hash to a slice in memory before sorting.
  * **Fix Strategy**: Stream row hashes into a temporary file or SQLite DB, or push the hashing down to the database using `MD5()/SHA2()`/`GROUP_CONCAT()` where possible. If client-side sorting is strictly required, use external merge sort.

## 3. Correctness & Data Safety

* **Conflict Policies**: Cleanly separated into `fail`, `source-wins`, `dest-wins`. `fail` correctly asserts `RowsAffected` to catch silent non-updates (good safety mechanism) `@/Volumes/UserData/Works/dbmigrate/internal/replicate/binlog/run.go:586`.
* **Zero Date Defaults**: Precheck implemented correctly, catching `sql_mode` mismatches with zero dates.
* **Collations**: Precheck ensures collations exist on the target.
* **Empty Destination Enforcement**: `migrate` correctly enforces an empty destination by default (`opts.DestEmptyRequired`), preventing accidental overwrites.

## 4. Incremental Replication Review

* **Binlog Format**: Correctly asserts `binlog_format=ROW` and `binlog_row_image=FULL` `@/Volumes/UserData/Works/dbmigrate/internal/replicate/binlog/run.go:291-296`.
* **Transaction Boundaries**: Batching mechanism preserves transaction boundaries (`XID_EVENT` / `COMMIT`) `@/Volumes/UserData/Works/dbmigrate/internal/replicate/binlog/load.go:220`.
* **DDL Handling**: The `classifyDDL` function (`@/Volumes/UserData/Works/dbmigrate/internal/replicate/binlog/load.go:696-743`) correctly tags `DROP`, `TRUNCATE`, `RENAME` as risky, but considers `ALTER TABLE ... ADD ...` as safe. This is generally okay, but could block on target. The default is safely `warn`.
* **GTID Support**: Currently missing (`opts.StartFrom == "gtid"` errors out). Needs implementation for robust failover scenarios.

## 5. Verification Review

* **Schema Normalization**: Solid implementation separating representation risks (JSON, temporal, float) from definitive hashes.
* **Timezone Safety**: Correctly sets session timezone to `+00:00` before extracting hashes `@/Volumes/UserData/Works/dbmigrate/internal/verify/data/verify.go:695`.
* **False Positives/Negatives**: The client-side sorting of row hashes (`@/Volumes/UserData/Works/dbmigrate/internal/verify/data/verify.go:495` `sort.Strings(rowHashes)`) prevents false positives from database engine row return order. However, it scales poorly (see OOM High-Risk finding).

## 6. Compatibility & Transformations

* **Version Diffs**: Supports MySQL to MariaDB and vice versa via standard go-mysql.
* **Definers**: Definer normalization during schema migrate is currently delegated to standard string replacements, but `verify` seems to catch definition mismatches.
* **Invisible/Generated PKs**: Dedicated precheck `runInvisibleGIPKPrecheck` exists, highlighting careful consideration of MySQL 8+ features.

## 7. Performance & Scalability

* **Streaming & Backpressure**: The baseline copy batches rows, but uses `LIMIT / OFFSET`. `OFFSET N` scans `N` rows before returning, creating $O(N^2)$ complexity for the full table copy. This will severely degrade performance on large tables.
* **Memory Bounds**: As noted, verification holds all row hashes in memory.
* **Concurrency**: Missing parallel table copy. Baseline copies tables sequentially (`@/Volumes/UserData/Works/dbmigrate/internal/data/copy.go:93`). Needs goroutine worker pool for concurrent table copy.

## 8. Reliability & Observability

* **Logging/Progress**: Emits structured human-readable (and optionally JSON) output (`@/Volumes/UserData/Works/dbmigrate/internal/commands/verify.go:267`).
* **Metrics**: Missing Prometheus/OpenTelemetry metrics.
* **Retries/Backoff**: No automatic reconnect or retry backoff observed if the database drops connections during `copy` or `replicate`.
* **Timeouts**: `OpenAndPing` sets a 5s connection timeout, but long-running queries have no context timeout bounds, risking hung processes.

## 9. Security Review

* **Secrets**: DSNs are redacted in output via `RedactDSN` (`@/Volumes/UserData/Works/dbmigrate/internal/db/connector.go:38`).
* **TLS**: Supports `?tls=preferred` or required modes explicitly parsed in DSN normalization.
* **Least Privilege**: Does not attempt to self-escalate; relies on user DSN.
* **Injection Risks**: Identifiers are safely quoted using `quoteIdentifier` (`@/Volumes/UserData/Works/dbmigrate/internal/data/copy.go:572`). Values are safely parameterized using `?` placeholders.

## 10. Testing Review

* **Integration Tests**: Strong presence of Docker Compose and e2e rehearsal scripts (`scripts/run-verify-canonicalization-rehearsal.sh`, etc).
* **Determinism**: The `MIGRATION_TESTING.md` specifies strict requirements, but unit tests (e.g., `internal/replicate/binlog/run_test.go`) use mocked interfaces rather than real databases.
* **Coverage Gaps**: E2E tests exist, but chaos testing (killing the container mid-migration and testing resume) appears missing or manually executed.

## 11. CI/CD & Release Review

* **CI Checks**: `.github/workflows/ci.yml` correctly runs `gofmt`, `go test`, `golangci-lint`, and `govulncheck`.
* **Artifacts**: Missing artifact build matrix (e.g., via Goreleaser) for Linux/Mac/Windows arm64/amd64.
* **SBOM/Checksums**: Missing SBOM generation and sha256 sums for binaries.

## 12. API/CLI UX Review

* **Flags**: Flags are well defined and segregated into Global and Subcommand domains (`@/Volumes/UserData/Works/dbmigrate/internal/cli/cli.go:105-167`).
* **Exit Codes**: Implements exact exit codes (`ExitCodeDiff`, etc.) to allow bash scripting integration.
* **Dry Run**: Excellent dry-run architecture utilizing a sandbox database to validate DML/DDL before applying it (`@/Volumes/UserData/Works/dbmigrate/internal/commands/migrate.go:229`).

## 13. Documentation Review

* **Runbooks**: `docs/operators-guide.md` and `docs/known-problems.md` are thorough and excellent for production operations.
* **Risk Checklist**: Present and captures edge cases like trigger side-effects and GTID drift.

## 14. Recommended Refactor Plan

1. **Phase 1: Safe Baseline Migration**
   * PR: Implement keyset pagination (`WHERE pk > ? ORDER BY pk`) in `internal/data/copy.go` to replace `LIMIT/OFFSET`.
   * PR: Wrap baseline schema & data extraction in `START TRANSACTION WITH CONSISTENT SNAPSHOT`.
2. **Phase 2: Scalable Verification**
   * PR: Refactor `VerifyFullHash` to use database-side hashing (`MD5/SHA2` combined with `GROUP_CONCAT` or checksum tables) or stream hashes to disk.
3. **Phase 3: Atomic Checkpointing**
   * PR: Create a `dbmigrate_checkpoint` table on the destination.
   * PR: Write binlog positions to this table inside the *same* destination transaction as the replication batch.
4. **Phase 4: Concurrency & Resiliency**
   * PR: Add goroutine worker pools to copy tables concurrently in baseline migration.
   * PR: Add automatic connection retries with exponential backoff.

## 15. Quick Wins

1. **`internal/data/copy.go:414`** - Immediately warn users if table has no PK, as baseline copy might be corrupted.
2. **`internal/replicate/binlog/run.go:548`** - Ensure `ctx` has a timeout to prevent hanging on metadata locks.
3. **`.github/workflows/ci.yml:50`** - Add a step to actually build the binary `go build ./cmd/dbmigrate`.
4. **`internal/replicate/binlog/load.go:299`** - Add support for `--start-from=gtid` via `go-mysql`'s GTID sets.
5. **`internal/cli/cli.go:122`** - Add a `--metrics-port` flag to expose Prometheus metrics.
6. **`internal/verify/data/verify.go:495`** - If `len(rowHashes) > 1_000_000`, print a warning about memory consumption.
7. **`internal/data/copy.go:133`** - Add a periodic progress logger (e.g., every 10k rows) during large table copies.
8. **`internal/db/connector.go:77`** - Increase ping timeout context slightly, 5s might fail on slow VPNs to RDS.
9. **`internal/commands/verify.go:120`** - Add a `diff` output mode that shows the actual missing keys/rows if sample fails.
10. **`internal/commands/replicate.go:68`** - Remove the blocking error for `gtid` once it is implemented.
