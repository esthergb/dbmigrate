---
name: dbmigrate-phase-delivery
description: Plan and execute dbmigrate work in milestone-based, PR-sized increments with strict sequencing, branch discipline, and completion gates. Use when implementing any repository change that must follow the required phase order, commit conventions, and continuity tracking.
---

# dbmigrate phase delivery

## Execute in this order

1. Read `Instructions.md`, `AGENTS.md`, and `CONTINUITY.md`.
2. Update `CONTINUITY.md` with current goal, constraints, and active state.
3. Select exactly one milestone-sized unit from [references/phases.md](references/phases.md).
4. Create a branch using required naming.
5. Implement only the selected unit with tests and docs.
6. Run verification commands relevant to that unit.
7. Update `CONTINUITY.md` with done/now/next.

## Enforce guardrails

- Keep changes minimal and coherent per PR.
- Preserve public CLI/API behavior unless the milestone explicitly changes it.
- Fail fast on ambiguous requirements; record assumptions as `UNCONFIRMED` in `CONTINUITY.md`.
- Never push or open PR without explicit user confirmation.

## Apply commit contract

- Use Conventional Commit messages.
- Include testing evidence in commit/PR notes.
- Record compatibility implications for MariaDB/MySQL variants.

## Completion checklist per unit

- Code implemented.
- Tests added/updated and run.
- Docs updated when behavior/operator workflow changes.
- Risks and follow-up work captured in `CONTINUITY.md`.
