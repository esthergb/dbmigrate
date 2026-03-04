# dbmigrate

`dbmigrate` is a Go CLI for migration and consistency verification between MySQL-family databases, with planned incremental replication support.

## Current status

This repository is in phased development.

Completed:
- Phase 0 research docs (`docs/known-problems.md`, `docs/risk-checklist.md`)
- Phase 1 foundation scaffold (CLI skeleton, project structure, CI baseline)

In progress next:
- Compatibility planning engine
- Baseline schema/data migration implementation

## Supported migration priorities

1. MariaDB -> MariaDB (upgrade/downgrade)
2. MySQL -> MySQL (upgrade/downgrade)
3. MariaDB <-> MySQL cross-engine

## Commands (scaffold)

```bash
dbmigrate plan --source "mysql://user:pass@host:3306/" --dest "mysql://user:pass@host:3306/"
dbmigrate migrate --source "mysql://..." --dest "mysql://..."
dbmigrate replicate --source "mysql://..." --dest "mysql://..."
dbmigrate verify --source "mysql://..." --dest "mysql://..."
dbmigrate report --source "mysql://..." --dest "mysql://..." --json
```

## Configuration file support (phase 2)

Use `--config <path>` to load YAML/JSON runtime options.
When both are present, explicit CLI flags override config-file values.

## Build

```bash
go build -trimpath -ldflags="-s -w" -o bin/dbmigrate ./cmd/dbmigrate
```

## Local checks

```bash
make fmt
make test
```

If `golangci-lint` and `govulncheck` are installed:

```bash
make lint
make vulncheck
```

## Safety notes

- Incompatible features are designed to fail fast.
- DDL application policy is controlled only by `--apply-ddl={ignore,apply,warn}`.
- Detailed migration risks and mitigations are documented in [docs/known-problems.md](docs/known-problems.md).

## Documentation

- [Development plan](docs/development-plan.md)
- [Known migration problems](docs/known-problems.md)
- [Operator risk checklist](docs/risk-checklist.md)
- [Operators guide](docs/operators-guide.md)
- [Security notes](docs/security.md)
