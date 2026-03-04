# dbmigrate Agent Playbook

This file defines how coding agents should execute work in this repository.

## Mission

Build `dbmigrate` as a production-grade Go CLI for MySQL/MariaDB migration with:
- baseline migration,
- incremental replication,
- schema/data verification,
- operator-safe defaults,
- strong tests and documentation.

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
- User/grant migration: implement now with selectable scope:
  - business accounts only,
  - or include system accounts.
- Auth/plugin incompatibilities: include detailed report output.
- Reports: support both redacted and non-redacted value output modes.
- CI: minimal tests in CI, full matrix locally.
- Before any remote push/PR creation, ask user for explicit confirmation.

## Branching and Commits

- Never commit directly to `main`.
- Create one branch per feature/fix/chore.
- Branch naming: `feat/<scope>-<short>`, `fix/<scope>-<short>`, `chore/<scope>-<short>`.
- Use Conventional Commits.
- Keep each PR focused, tested, and documented.

## Required Execution Order

1. Create and maintain `CONTINUITY.md`.
2. Complete migration research docs before coding:
  - `docs/known-problems.md`
  - `docs/risk-checklist.md`
3. Set repository scaffolding and CI.
4. Implement feature phases in order with tests per phase.
5. Update operator/developer docs continuously.

## Dependency Policy

Prefer highly adopted, stable dependencies with clear maintenance.
Minimize dependency count. Document every dependency rationale in code/docs.

## Testing Policy

- Always run smallest meaningful local tests while developing.
- CI runs minimal guardrail suite (lint, unit, smoke-level integration).
- Full Docker matrix is mandatory locally before release-level milestones.
- Apple Silicon compatibility is required for local matrix execution.

## Skills to Use

Use the following local skills when applicable:
- `skills/dbmigrate-phase-delivery`
- `skills/dbmigrate-research-risk`
- `skills/dbmigrate-test-matrix`

If multiple skills apply, use them in this order:
1. phase delivery
2. research-risk
3. test-matrix
