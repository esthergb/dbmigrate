# dbmigrate Agent Playbook

This file defines how coding agents should execute work in this repository.

## Mission

Build `dbmigrate` as a production-grade Go CLI for MySQL/MariaDB migration with:
- baseline migration,
- incremental replication,
- schema/data verification,
- operator-safe defaults,
- strong tests and documentation.

## Current State (as of PR #85)

v1 core is implemented and passing all tests. The following is done:

- **5 subcommands:** `plan`, `migrate`, `replicate`, `verify`, `report`.
- **15+ prechecks:** collation, plugins, GIPK, zero-dates, FK cycles, identifiers, temporal, replication boundary, schema features, parser drift, lower_case_table_names, timezone, data shape.
- **Binlog replication** with checkpoints, conflict policies, DDL handling, bounded buffering.
- **4 data verification modes:** count, hash, sample, full-hash.
- **Checkpoint/resume** for baseline data and replication.
- **Dry-run sandbox** for schema validation.
- **CI:** build + fmt + test + lint + vulncheck + smoke integration.
- **Docker matrix:** 14 services covering MariaDB 10.6/10.11/11.0/11.4/11.8/12.0 and MySQL 8.0/8.4.
- **3 direct dependencies only:** go-mysql-org/go-mysql, go-sql-driver/mysql, gopkg.in/yaml.v3.

What remains for v2:
- Routines/triggers/events migration.
- Trigger-based CDC and hybrid replication mode.
- GTID support as start-from.
- User/grant migration.
- Real concurrency in data copy.
- Structured JSON logging.
- Granular schema verification (columns, indexes, FK, partitions).

## Architecture Map

```text
cmd/dbmigrate/main.go          → entry point
internal/cli/cli.go             → flag parsing, subcommand dispatch, timeout
internal/commands/               → subcommand handlers (plan, migrate, replicate, verify, report)
internal/commands/*_precheck.go  → 15+ precheck modules
internal/config/runtime.go       → RuntimeConfig, flag binding, YAML config
internal/db/                     → DSN building, TLS config, driver wiring
internal/schema/copy.go          → schema extraction/apply, FK ordering, sandbox
internal/data/copy.go            → chunked data copy, checkpoint/resume, watermark
internal/replicate/binlog/       → binlog streaming, event conversion, batch apply
internal/verify/schema/          → SHOW CREATE normalization and diff
internal/verify/data/            → count/hash/sample/full-hash verification
internal/compat/evaluate.go      → version matrix, downgrade profiles, findings
internal/state/                  → checkpoint/artifact persistence, file locking
internal/version/                → build version constant
```

Key patterns:
- **Dependency injection** via package-level function vars (e.g. `streamWindowEventsFn`, `loadTableMetadataFn`) for testability.
- **JSON + text dual output** via `cfg.JSON` toggle in every command.
- **State-dir artifacts** for audit trail: prechecks persist JSON, `report` aggregates them.
- **Exit codes:** 0=success, 1=usage, 2=diffs/incompatibilities, 3=runtime failure, 4=verify failure.
- **Findings model:** all prechecks emit `[]compat.Finding` with code, severity, message, proposal.

## Delivery Strategy

Use phased delivery with PR-sized increments.

Phase priority:
1. MariaDB -> MariaDB (upgrade/downgrade)
2. MySQL -> MySQL (upgrade/downgrade)
3. MariaDB <-> MySQL cross-engine

Do not start implementation until prerequisite phase documentation is ready.

## Hard Decisions (Confirmed)

- License: MIT.
- Docs language: English.
- DDL control flag: `--apply-ddl={ignore,apply,warn}` only.
- Incompatible features: fail fast for now; design auto-fix as future roadmap.
- Conflict policy default: `fail`.
- User/grant migration: implement with selectable scope (business accounts only, or include system accounts).
- Auth/plugin incompatibilities: include detailed report output.
- Reports: support both redacted and non-redacted value output modes.
- CI: minimal tests in CI, full matrix locally.
- Before any remote push/PR creation, ask user for explicit confirmation.
- v1 scope: tables/views only, binlog replication only, self-managed only.
- v2 scope: routines/triggers/events, trigger-CDC, hybrid, GTID, user/grant, concurrency.
- v3 scope: managed/cloud deployments.

## Branching and Commits

- Never commit directly to `main`.
- Create one branch per feature/fix/chore.
- Branch naming: `feat/<scope>-<short>`, `fix/<scope>-<short>`, `chore/<scope>-<short>`.
- Use Conventional Commits.
- Keep each PR focused, tested, and documented.

## Required Execution Order

For new features:

1. Update `CONTINUITY.md` with goal and constraints.
2. Verify prerequisite docs/research exist for the feature domain.
3. Implement with tests, following existing patterns:
   - dependency injection for testability,
   - dual JSON/text output,
   - state-dir artifact persistence,
   - findings model for prechecks.
4. Run `go test ./... -count=1` and `go vet ./...` before committing.
5. Update operator/developer docs when behavior changes.
6. Update `CONTINUITY.md` with done/now/next.

## Code Conventions

- **Imports** at the top of file, never in the middle.
- **Error handling:** wrap with `fmt.Errorf("context: %w", err)`. Use `applyFailure` struct for replication errors with type, remediation, and context.
- **No comments unless explicitly requested.** Existing comments must be preserved.
- **Identifier quoting:** always use `quoteIdentifier()` (backtick escaping) for user-supplied identifiers.
- **SQL injection prevention:** always use `?` parameterized queries. Never interpolate user values into SQL strings.
- **Linting:** `.golangci.yml` enables errcheck, govet, staticcheck, ineffassign. All must pass.
- **File permissions:** state-dir files written with `0o600`.

## Dependency Policy

Prefer highly adopted, stable dependencies with clear maintenance.
Minimize dependency count. Document every dependency rationale in code/docs.

Current direct dependencies (do not add without strong justification):
- `github.com/go-mysql-org/go-mysql` — binlog streaming.
- `github.com/go-sql-driver/mysql` — database/sql driver.
- `gopkg.in/yaml.v3` — config file parsing.

## Testing Policy

- Always run smallest meaningful local tests while developing.
- CI runs minimal guardrail suite (lint, unit, smoke-level integration).
- Full Docker matrix is mandatory locally before release-level milestones.
- Apple Silicon compatibility is required for local matrix execution.
- Use dependency injection (function vars) for unit testing without real DB connections.
- Integration test scripts live in `scripts/` and use `configs/` + `testdata/`.

## Skills to Use

Use the following local skills when applicable:

| Skill | When to use |
| ----- | ----------- |
| `skills/dbmigrate-phase-delivery` | Planning and executing any PR-sized milestone |
| `skills/dbmigrate-research-risk` | Pre-implementation research for new feature domains |
| `skills/dbmigrate-test-matrix` | Running smoke/full matrix validation |
| `skills/dbmigrate-v1-release` | v1 release gate execution and signoff |
| `skills/dbmigrate-schema-objects` | Implementing routines/triggers/events migration and verification |
| `skills/dbmigrate-replication-cdc` | Implementing trigger-CDC, hybrid mode, and GTID support |
| `skills/dbmigrate-code-quality` | Structured logging, concurrency, performance patterns |

If multiple skills apply, use them in this order:
1. phase delivery
2. research-risk
3. domain-specific skill (schema-objects, replication-cdc, code-quality)
4. test-matrix
5. v1-release
