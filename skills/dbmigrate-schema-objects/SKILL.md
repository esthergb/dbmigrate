---
name: dbmigrate-schema-objects
description: Implement migration and verification of stored procedures, functions, triggers, and events. Use when extending dbmigrate beyond tables/views to support full schema object migration with cross-engine compatibility handling.
---

# dbmigrate schema objects migration

## Objective

Extend the existing schema migration and verification to cover stored procedures, functions, triggers, and events — completing the schema object coverage gap identified in the v1 review.

## Architecture context

Current implementation covers tables and views only:

- `internal/schema/copy.go` — `extractCreateStatements()` queries `INFORMATION_SCHEMA.TABLES` for `BASE TABLE` and `VIEW` types, then applies via `SHOW CREATE TABLE/VIEW`.
- `internal/verify/schema/verify.go` — `listObjectDefinitions()` compares normalized `SHOW CREATE` output.
- `internal/config/runtime.go` — `--include-objects` flag accepts CSV, currently only `tables,views`.
- `internal/commands/migrate.go` — passes `IncludeTables`/`IncludeViews` booleans to schema copy.

## Implementation plan

### PR 1: Routine extraction and apply

1. Add `queryRoutineNames()` in `internal/schema/copy.go`:
   - Query `INFORMATION_SCHEMA.ROUTINES` with `ROUTINE_TYPE IN ('PROCEDURE','FUNCTION')`.
   - Filter by `ROUTINE_SCHEMA`.

2. Add `fetchRoutineCreateStatement()`:
   - Use `SHOW CREATE PROCEDURE/FUNCTION schema.name`.
   - Handle the multi-column result (Name, sql_mode, Create Procedure/Function, ...).
   - Extract the CREATE statement from the correct column.

3. Extend `extractCreateStatements()` to include routines when enabled.

4. Handle DEFINER rewriting:
   - Reuse existing `definerClauseRe` regex for `DEFINER=CURRENT_USER` substitution.
   - Apply in `sanitizeCreateStatementForApply()`.

5. Handle delimiter edge cases:
   - Routines may contain `;` in body.
   - Use `database/sql` `ExecContext` which handles full statement — no delimiter issue at driver level.

6. Add `IncludeRoutines bool` to `CopyOptions` and `DryRunSandboxOptions`.

7. Update `--include-objects` parsing in `internal/config/runtime.go` to accept `routines`.

### PR 2: Trigger extraction and apply

1. Add `queryTriggerNames()`:
   - Query `INFORMATION_SCHEMA.TRIGGERS` filtered by `TRIGGER_SCHEMA`.

2. Add `fetchTriggerCreateStatement()`:
   - Use `SHOW CREATE TRIGGER schema.name`.

3. Apply order constraint:
   - Triggers must be applied AFTER their base tables exist.
   - Current FK-ordered table apply handles this naturally if triggers are appended after tables.

4. Add `IncludeTriggers bool` to options.

5. Cross-engine considerations:
   - MariaDB supports `OR REPLACE` in triggers; MySQL does not.
   - MariaDB supports multiple triggers per timing/event; MySQL 8.0+ does too.
   - Strip `OR REPLACE` when destination is MySQL.

### PR 3: Event extraction and apply

1. Add `queryEventNames()`:
   - Query `INFORMATION_SCHEMA.EVENTS` filtered by `EVENT_SCHEMA`.

2. Add `fetchEventCreateStatement()`:
   - Use `SHOW CREATE EVENT schema.name`.

3. Cross-engine considerations:
   - Both MySQL and MariaDB support events with similar syntax.
   - DEFINER rewriting applies.
   - Event scheduler state (`@@event_scheduler`) should be checked as a precheck.

4. Add `IncludeEvents bool` to options.

### PR 4: Schema verification for new object types

1. Extend `internal/verify/schema/verify.go`:
   - Add `listRoutineDefinitions()`, `listTriggerDefinitions()`, `listEventDefinitions()`.
   - Use same normalized comparison pattern as tables/views.
   - Add object types: `procedure`, `function`, `trigger`, `event`.

2. Add normalization rules:
   - Strip DEFINER clause.
   - Normalize whitespace.
   - Handle `OR REPLACE` differences.

### PR 5: Cross-engine prechecks

1. Add precheck module `internal/commands/routine_precheck.go`:
   - Detect routines using MySQL-specific syntax unsupported on MariaDB (and vice versa).
   - Check for incompatible SQL modes in routine bodies.
   - Emit `compat.Finding` with severity and remediation.

2. Integrate into `plan` and `migrate` precheck pipeline.

## Testing strategy

- Unit tests with dependency-injected DB queries (function vars pattern).
- Integration tests covering:
  - Same-engine routine migration (MariaDB → MariaDB, MySQL → MySQL).
  - Cross-engine with DEFINER rewriting.
  - Triggers on tables with FK dependencies.
  - Events with scheduler state validation.
- Add test SQL fixtures in `testdata/` with representative routines.

## Key risks

- **Body parsing:** routine bodies can contain arbitrary SQL. Never parse the body — use `SHOW CREATE` as opaque DDL.
- **DEFINER conflicts:** always rewrite to `CURRENT_USER` unless user explicitly opts out.
- **Execution order:** triggers depend on tables; events are independent; routines may depend on other routines (no guaranteed ordering from INFORMATION_SCHEMA).
- **Cross-engine syntax:** MariaDB `RETURNS` vs MySQL `RETURNS` for functions is identical, but `DETERMINISTIC`/`SQL SECURITY` defaults may differ.
