---
name: dbmigrate-research-risk
description: Perform pre-implementation research for MySQL/MariaDB migration pitfalls and convert findings into enforceable safeguards and operator checklists. Use before coding migration or replication features and whenever compatibility assumptions need re-validation.
---

# dbmigrate research and risk

## Objective

Create actionable research outputs that directly drive tool safeguards:
- `docs/known-problems.md`
- `docs/risk-checklist.md`

## Workflow

1. Collect primary sources for each mandatory risk domain listed in [references/research-checklist.md](references/research-checklist.md).
2. Extract concrete failure modes, not generic advice.
3. Map each failure mode to one of:
   - preflight validation,
   - runtime guardrail,
   - operator warning,
   - explicit unsupported-case failure.
4. Record mitigation behavior that can be implemented in code.
5. Keep docs concise, traceable, and implementation-oriented.

## Source quality rules

- Prefer official MySQL/MariaDB docs and well-known vendor knowledge bases.
- Add issue/community links only when they illustrate real operational breakages.
- Include links for every significant claim.

## Output contract

- Every problem entry includes: symptom, root cause, affected versions/flavors, detection method, mitigation strategy.
- Every checklist item includes: what to verify, why it matters, how to validate quickly.
- Mark unknowns explicitly; do not infer unsupported behavior.
