# Contributing to dbmigrate

## Branch and PR workflow

- `main` is protected; do not commit directly.
- Use one branch per feature/fix/chore:
  - `codex/feat/<scope>-<short>`
  - `codex/fix/<scope>-<short>`
  - `codex/chore/<scope>-<short>`
- Use Conventional Commits (`feat:`, `fix:`, `docs:`, `test:`, `chore:`).
- Open a PR for each branch and keep scope small.

## Required PR content

Each PR must include:
- summary of changes,
- testing evidence,
- compatibility notes (MariaDB/MySQL implications),
- risks and follow-ups.

## Local development

```bash
go test ./... -count=1
```

Optional tooling:

```bash
gofmt -w ./cmd ./internal
golangci-lint run ./...
govulncheck ./...
```

## CI policy

- CI runs minimal guardrails: format check, unit tests, lint, vulnerability scan.
- Full engine/version matrix runs locally before release milestones.

## Security and secrets

- Never commit credentials or private keys.
- Avoid logging DSN secrets.
- Prefer environment variables or secret managers for runtime credentials.
