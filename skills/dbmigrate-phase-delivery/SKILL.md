---
name: dbmigrate-phase-delivery
description: Plan and execute dbmigrate work in milestone-based, PR-sized increments with strict sequencing, branch discipline, and completion gates. Use when implementing any repository change that must follow the required phase order, commit conventions, and continuity tracking.
---

# dbmigrate phase delivery

## Execute in this order

1. Read `AGENTS.md` (architecture map + code conventions) and `CONTINUITY.md`.
2. Update `CONTINUITY.md` with current goal, constraints, and active state.
3. Select exactly one milestone-sized unit from [references/phases.md](references/phases.md).
4. If a domain-specific skill exists (schema-objects, replication-cdc, code-quality), read it first.
5. Create a branch using required naming: `feat/<scope>-<short>`, `fix/<scope>-<short>`, `chore/<scope>-<short>`.
6. Implement only the selected unit with tests and docs.
7. Run `go test ./... -count=1` and `go vet ./...`.
8. Update `CONTINUITY.md` with done/now/next.

## Enforce guardrails

- Keep changes minimal and coherent per PR.
- Preserve public CLI/API behavior unless the milestone explicitly changes it.
- Fail fast on ambiguous requirements; record assumptions as `UNCONFIRMED` in `CONTINUITY.md`.
- Never push or open PR without explicit user confirmation.

## Follow existing patterns

- **Dependency injection:** use package-level function vars (e.g. `var myFuncFn = myFunc`) for testability.
- **Dual output:** support both JSON and text via `cfg.JSON` toggle.
- **State-dir artifacts:** persist JSON artifacts; `report` command aggregates them.
- **Findings model:** prechecks emit `[]compat.Finding` with code, severity, message, proposal.
- **Error handling:** wrap with `fmt.Errorf("context: %w", err)`. Use `applyFailure` for replication errors.
- **Identifier safety:** use `quoteIdentifier()` and `?` parameterized queries.

## Apply commit contract

- Use Conventional Commit messages.
- Include testing evidence in commit/PR notes.
- Record compatibility implications for MariaDB/MySQL variants.

## Completion checklist per unit

- Code implemented following patterns above.
- Tests added/updated and run (`go test ./... -count=1`).
- Linting passes (`go vet ./...` + golangci-lint).
- Docs updated when behavior/operator workflow changes.
- Risks and follow-up work captured in `CONTINUITY.md`.
