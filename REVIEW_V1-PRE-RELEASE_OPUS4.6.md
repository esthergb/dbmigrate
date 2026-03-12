# dbmigrate v1 Pre-Release Code Review

**Reviewer:** Opus 4.6 (AI-assisted)
**Date:** 2026-03-07
**Scope:** `internal/commands/` — all 24 Go source files

---

## 1. Core Infrastructure

### `commands.go`

Registry mapping command names (`plan`, `migrate`, `replicate`, `verify`, `report`) to their `Handler` functions. Each handler has signature:

```go
type Handler func(ctx context.Context, cfg config.RuntimeConfig, args []string, out io.Writer) error
```

`Synopsis()` returns a one-line description per command.

### `output.go`

Shared `writeResult` helper for JSON/text output with `commandResult` struct carrying command name, status, message, timestamp, and version. Also provides `WriteVersion` for the `--version` flag.

### `exit_error.go`

Custom `ExitError` type carrying process exit codes:

- `ExitCodeDiff = 2` — successful execution but incompatibilities/differences detected.
- `ExitCodeVerifyFailed = 4` — verify command internal/runtime failure.

Helpers `WithExitCode` and `ResolveExitCode` for structured exit handling. `WithExitCode` avoids double-wrapping if the error already carries an exit code.

---

## 2. Main Commands

### `plan.go` (191 lines)

Implements the `plan` command — a compatibility precheck suite that connects to source and destination databases, detects server versions, and runs multiple prechecks:

- Zero-date defaults (temporal precheck)
- Plugin lifecycle (auth plugins + storage engines)
- Invisible columns / GIPK
- Collation compatibility

Outputs a `planResult` struct with per-precheck summaries and aggregated findings as JSON or text. Marks the plan as `incompatible` if any precheck sets its `Incompatible` flag.

### `migrate.go` (448 lines)

Implements the `migrate` command for baseline schema and data migration.

**Options parsed via `parseMigrateOptions`:**

- `--schema-only`, `--data-only`
- `--chunk-size` (default 10000)
- `--resume` (resume from checkpoint)
- `--dry-run` with sandbox mode

**Key flow (`runMigrate`):**

1. Opens source/dest DB connections.
2. Runs the same precheck suite as `plan` (zero-date, plugin, invisible/GIPK, collation).
3. If any precheck is incompatible, writes the precheck report and exits with `ExitCodeDiff`.
4. In `dry-run=sandbox` mode: creates sandbox databases on destination, runs schema migration against them, reports results, and cleans up.
5. Otherwise: orchestrates schema copy then data copy via internal packages.

Sandbox helper functions: `runMigrateDryRunSandbox`, `cleanupSandboxDatabases`, `sandboxDatabaseName`.

### `replicate.go` (220 lines)

Implements the `replicate` command for binlog-based incremental replication.

**Options parsed:**

- `--replication-mode` (binlog only; GTID and trigger-based CDC are unimplemented and fail fast)
- `--start-position` (binlog file + position)
- `--conflict-policy`
- `--dry-run`

**Key flow (`runReplicate`):**

1. Parses and validates replication options.
2. Opens source/dest DB connections.
3. Delegates to `binlog.Run()` for the actual replication loop.
4. In dry-run mode: outputs status summary without applying changes.

### `verify.go` (389 lines)

Implements the `verify` command for schema and data consistency checking.

**Options:**

- `--verify-level` (`schema` or `data`)
- `--data-mode` (`count`, `hash`, `sample`, `full-hash`)

**Key flow (`runVerify`):**

- Schema level: calls `schemaVerify.Verify()`, outputs diffs with risk annotations.
- Data level: dispatches to the appropriate data verification mode (`VerifyCount`, `VerifyHash`, `VerifySample`, `VerifyFullHash`).
- Persists data verification artifact to state dir.
- Returns `ExitCodeDiff` if diffs found, `ExitCodeVerifyFailed` on runtime errors.

Outputs `verifySchemaResult` or `verifyDataResult` as JSON or text.

### `report.go` (554 lines)

Implements the `report` command — aggregates state-dir artifacts into a unified migration status report.

**Artifacts loaded:**

- Collation precheck report
- Verify data report
- Data baseline checkpoint
- Replication checkpoint
- Replication conflict reports

**Key logic:**

- Determines overall status (`ok`, `warn`, `blocked`, `error`).
- Generates proposals for remediation based on artifact state.
- Detects stale conflict reports via `isStaleConflictReport` (compares conflict binlog position against replication checkpoint).
- Helper `compareBinlogFile` for binlog file ordering.
- `summarizeDataCheckpoint` for baseline migration progress.

---

## 3. Precheck Modules

### `temporal_precheck.go` (387 lines)

**Zero-date defaults precheck.** Detects source columns with `0000-00-00` style defaults that would be rejected by destination strict `sql_mode`.

**Flow:**

1. Queries destination `@@SESSION.sql_mode`.
2. Checks for `STRICT_TRANS_TABLES`/`STRICT_ALL_TABLES` combined with `NO_ZERO_DATE`/`NO_ZERO_IN_DATE`.
3. If enforced: inventories source temporal columns with zero-date defaults.
4. Auto-generates `ALTER TABLE ... SET DEFAULT` fix SQL per column.
5. Writes fix script to state dir (`precheck-zero-date-fixes.sql`).

**Shared utilities defined here:**

- `querySQLMode` — reads `@@SESSION.sql_mode`
- `sqlModeContains` — token-level sql_mode check
- `listDatabases` — enumerates `INFORMATION_SCHEMA.SCHEMATA`
- `temporalDefaultContainsZeroDate` — parses date strings for zero components

### `collation_precheck.go` (519 lines)

**Collation compatibility precheck.** Inventories source collations at schema, table, and column scope and checks them against the destination's supported collations.

**Two detection passes:**

1. **Unsupported destination collations** — source collations not present in destination `INFORMATION_SCHEMA.COLLATIONS`. Sets `Incompatible = true`.
2. **Client compatibility risks** — UCA1400 family collations (`utf8mb4_uca1400_*`, `uca1400_*`) that servers may accept but client libraries may not understand. Warning-level only.

**Notable implementation details:**

- Handles MariaDB UCA1400 prefix aliasing: `uca1400_*` ↔ `utf8mb4_uca1400_*`.
- `dedupeCollationIssues` prevents duplicate findings via composite key.
- Persists JSON artifact to state dir for later consumption by `report`.
- Cleanup removes artifact when no issues found.

**Proposals are context-aware:**

- MySQL 0900 collations → suggest MariaDB equivalent mapping.
- MariaDB UCA1400 collations → suggest MySQL equivalent mapping.
- Generic fallback for other collations.

### `plugin_precheck.go` (542 lines)

**Plugin lifecycle precheck.** Two-pronged check covering authentication plugins and storage engines.

**Auth plugin check:**

- Queries `mysql.user` for source account plugins.
- Queries destination `INFORMATION_SCHEMA.PLUGINS` for active plugins.
- Detects unsupported auth plugins with per-plugin proposals (`mysql_native_password`, `caching_sha2_password`, `unix_socket`/`auth_socket`).
- Classifies accounts as `system`, `administrative`, or `user-managed`.

**Storage engine check:**

- Queries source `INFORMATION_SCHEMA.TABLES` for engine usage.
- Queries destination `INFORMATION_SCHEMA.ENGINES` for support status.
- Checks `NO_ENGINE_SUBSTITUTION` in destination sql_mode.
- Per-engine proposals (Aria, Federated/Connect, generic).
- Sets `Incompatible = true` if unsupported engines found.

**Additional checks:**

- `default_authentication_plugin` variable presence.
- Graceful degradation: metadata query failures produce warn-level findings rather than hard errors.

### `invisible_gipk_precheck.go` (488 lines)

**Invisible columns and Generated Invisible Primary Keys (GIPK) precheck.** MySQL-specific hidden-schema feature detection.

**Three inventories:**

1. **Invisible columns** — `EXTRA LIKE '%INVISIBLE%'` excluding GIPK `my_row_id` columns.
2. **Invisible indexes** — `IS_VISIBLE = 'NO'` in `INFORMATION_SCHEMA.STATISTICS`.
3. **GIPK tables** — primary key columns with both `INVISIBLE` and `AUTO_INCREMENT` in EXTRA.

**Version-gated destination support:**

- Invisible columns: MySQL ≥ 8.0.23
- Invisible indexes: MySQL ≥ 8.0.0
- GIPK: MySQL ≥ 8.0.30
- MariaDB destination: always incompatible for these MySQL features.

**Session variable checks (MySQL source only):**

- `@@show_gipk_in_create_table_and_information_schema` — warns if disabled (GIPK inventory may be incomplete).
- `@@sql_generate_invisible_primary_key` — informational.

**Severity escalation:**

- `info` when destination supports the features.
- `error` when destination cannot preserve invisible/GIPK semantics.

---

## 4. Artifact Persistence

### `verify_artifact.go` (74 lines)

Manages the `verify-data-report.json` artifact in state dir. Records command, status (`ok`/`diff`), verify level, data mode, summary (from `dataVerify.Summary`), timestamp, and version.

Used by `verify.go` after data verification and consumed by `report.go` for unified status.

---

## 5. Cross-Cutting Patterns

| Pattern | Usage |
|---------|-------|
| **JSON + text dual output** | Every command and precheck supports `cfg.JSON` toggle via `json.NewEncoder` or `fmt.Fprintf` |
| **State dir artifacts** | Prechecks persist JSON artifacts; `report` aggregates them |
| **Findings model** | All prechecks emit `[]compat.Finding` with `code`, `severity`, `message`, `proposal` |
| **Graceful degradation** | Metadata query failures → warn findings, not fatal errors |
| **Database filtering** | `schema.SelectDatabases(all, include, exclude)` used consistently |
| **`sqlPlaceholders` helper** | Shared parameterized query builder for `IN (?)` clauses |
| **Version gating** | `versionAtLeast(instance, major, minor, patch)` for feature support checks |
| **Artifact lifecycle** | persist → load → cleanup pattern with `os.Remove` + `os.IsNotExist` guard |
| **Exit codes** | `ExitCodeDiff=2` for incompatibilities, `ExitCodeVerifyFailed=4` for runtime errors |
| **Deduplication** | Composite-key dedup for collation issues; unique-set dedup for fix scripts |

---

## 6. Observations and Recommendations

### Strengths

1. **Consistent architecture** — all commands follow the same `Handler` signature, output pattern, and finding model.
2. **Operator-safe defaults** — prechecks block migration on incompatibilities rather than silently proceeding.
3. **Detailed proposals** — every finding includes actionable remediation guidance.
4. **Artifact-based workflow** — state dir artifacts create an audit trail and enable the `report` command to work offline.
5. **Graceful degradation** — permission or access errors in precheck queries produce warnings instead of aborting.
6. **Cross-engine awareness** — MariaDB ↔ MySQL differences are handled explicitly (collation aliasing, GIPK incompatibility, engine-specific features).

### Areas to Watch

1. **SQL injection surface in `sqlPlaceholders`** — uses `?` parameterized queries correctly, but `queryOptionalBooleanVariable` builds queries via `fmt.Sprintf("SELECT %s", expression)` where `expression` is a hardcoded `@@variable` name. Currently safe since callers pass string literals, but worth documenting the invariant.
2. **`autoFixAlterDefaultSQL` uses `quoteIdentifier`** — referenced but defined elsewhere. Verify it handles edge cases (backtick escaping in identifiers containing backticks).
3. **`invisible_gipk_precheck.go` line 172–177** — the GIPK exclusion filter (`COLUMN_NAME = 'my_row_id'`) is MySQL-convention-dependent. If a future MySQL version changes the generated column name, this filter breaks silently.
4. **Collation UCA1400 aliasing** — the bidirectional prefix normalization in `queryDestinationSupportedCollations` (lines 158–163) is pragmatic but could produce false positives if a collation named `utf8mb4_uca1400_something` exists alongside `uca1400_something` with different semantics.
5. **No timeout/context deadline enforcement** — precheck queries rely on the caller's `ctx` but don't set explicit query timeouts. Long-running `INFORMATION_SCHEMA` queries on large instances could block indefinitely.
6. **`report.go` complexity** — at 554 lines with multiple artifact loaders and status aggregation logic, this file may benefit from extraction into smaller focused units as the artifact set grows.
7. **Test coverage** — test files exist for all major modules. Verify that edge cases (empty database lists, permission denied, mixed-engine scenarios) are covered.

---

## 7. File Inventory

| File | Lines | Purpose |
|------|-------|---------|
| `commands.go` | 35 | Command registry and synopsis |
| `output.go` | 44 | Shared output helpers |
| `exit_error.go` | 56 | Exit code handling |
| `plan.go` | 191 | Plan/precheck orchestration |
| `migrate.go` | 448 | Baseline migration |
| `replicate.go` | 220 | Binlog replication |
| `verify.go` | 389 | Schema/data verification |
| `verify_artifact.go` | 74 | Verify data artifact persistence |
| `report.go` | 554 | Unified status reporting |
| `temporal_precheck.go` | 387 | Zero-date defaults precheck |
| `collation_precheck.go` | 519 | Collation compatibility precheck |
| `plugin_precheck.go` | 542 | Auth plugin + storage engine precheck |
| `invisible_gipk_precheck.go` | 488 | Invisible columns / GIPK precheck |
| `collation_precheck_test.go` | — | Tests for collation precheck |
| `invisible_gipk_precheck_test.go` | — | Tests for invisible/GIPK precheck |
| `migrate_test.go` | — | Tests for migrate command |
| `output_test.go` | — | Tests for output helpers |
| `plan_test.go` | — | Tests for plan command |
| `plugin_precheck_test.go` | — | Tests for plugin precheck |
| `replicate_test.go` | — | Tests for replicate command |
| `report_test.go` | — | Tests for report command |
| `temporal_precheck_test.go` | — | Tests for temporal precheck |
| `verify_test.go` | — | Tests for verify command |
| `exit_error_test.go` | — | Tests for exit error handling |

**Total implementation:** ~3,947 lines across 13 source files + 11 test files.
