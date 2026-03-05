# Database Migration Tool (dbmigrate)

You are an expert database migration engineer and senior Go software architect. Build a production-grade CLI tool called `dbmigrate` in **Go** that migrates MySQL-family databases from a **source server** to a **destination server**, supports **incremental replication** (run repeatedly to pull changes since the last successful run), and then **verifies full consistency** (schema + data). The tool must support cross-engine/version migration, especially:

* Source: MariaDB 10.x → Destination: MariaDB 11.x
* Source: MariaDB 10.x → Destination: MySQL 8.x
* Source: MySQL 8.0 → Destination: MySQL 8.4

Hard requirements:

* **Minimal dependencies** (keep Go module deps small and well-justified; avoid heavy frameworks).
* **Single binary** output (prefer static build when feasible, clearly document build flags and any OS caveats).
* Clean modular architecture, excellent error handling, structured logging, secure defaults, comprehensive tests, and great operator UX.

Return the **entire repository**: source code, tests, Docker setup, documentation, CI configuration, and a detailed development plan. Follow best practices for software engineering throughout.

---

## 0) BEFORE CODING: Thorough internet research of common migration/upgrade problems (REQUIRED)

Before writing any code, conduct a thorough internet search and compile a concise but thorough “Known Problems & Mitigations” document in the repo: `docs/known-problems.md`. Use real issues reported by DBAs/users during MariaDB/MySQL migrations/upgrades and replication changes. Include links and short explanations. Use this research to define safeguards, validations, warnings, and default behaviors in the tool.

At minimum cover these known problem areas and how the tool mitigates them:

1. MariaDB↔MySQL feature incompatibilities (JSON behavior/storage differences, syntax gaps, optimizer differences, sequences/system-versioned tables where present, etc.) and how to detect/plan around them using official docs.
2. Replication incompatibilities: MariaDB vs MySQL GTID differences (not compatible), when you must use file/position instead, and how cross-engine replication constraints affect your incremental strategy.
3. Collation/charset pitfalls (illegal mix of collations, upgrade collation changes, missing collations between versions). Add preflight checks and mapping strategy.
4. Authentication plugin gotchas when moving to MySQL 8+ (e.g., default auth plugin changes and older client issues); include preflight checks and an **optional** (off-by-default) user/grant migration helper.
5. Reserved-word and parser incompatibilities across versions (identifiers that become reserved). Add a scanner for object definitions and suggest quoting/rewriting.
6. Binlog format tradeoffs and limitations (ROW vs STATEMENT/MIXED), why ROW is preferred for accurate incremental replication, and how to detect/enforce.
7. Operational pain points: replication lag, large transactions, long-running alters, deadlocks, timeouts; ensure the tool has rate limiting, chunking, batching, retries with backoff, and clear metrics/progress.

Also add `docs/risk-checklist.md` with an operator checklist (privileges, binlog settings, disk, timeouts, SQL mode, charset/collation, etc.).

Only after these docs are created, proceed with implementation.

---

## 1) Git + GitHub workflow (MANDATORY)

Initialize and use a Git repository and implement a disciplined PR workflow.

Create local repo and set remote to:

* SSH remote: `git@github.com:esthergb/dbmigrate.git`
  Assume an SSH key is already available in the environment and should be used for Git operations. Do not print private key material; only show commands and expected setup.

Branching rules:

* `main` is protected; no direct commits.
* Create a branch for **every function/feature implemented** (small increments). Naming: `feat/<scope>-<short>`, `fix/<scope>-<short>`, `chore/<scope>-<short>`.
* Each branch must have a PR to `main`, gated by CI checks (tests, lint, formatting). Provide PR templates.

Commits:

* Use Conventional Commits (`feat:`, `fix:`, `test:`, `docs:`, `chore:`).
* Each PR must include: description, testing evidence, and any compatibility notes.

CI:

* Add GitHub Actions workflow to run: `gofmt`, `go test`, lints, integration tests (Docker Compose), dependency/security checks, and build artifacts (release-ready binary).

Include `CONTRIBUTING.md` describing this workflow and how to run tests locally.

---

## 2) Product scope and features

## Core goals

1. **Initial migration**: migrate schema + baseline data.
2. **Incremental replication**: run repeatedly and apply changes from source since last successful run.
3. **Verification**: ensure destination matches source (schema + data), configurable depth and performance.

---

## 3) CLI specification

Create CLI `dbmigrate` with subcommands:

### `dbmigrate plan`

* Computes a migration/replication plan and compatibility warnings.
* Outputs: human-readable + optional JSON.
* Includes checks: charset/collation compatibility, GTID/binlog requirements, reserved words, definers, SQL mode differences, object incompatibilities.

### `dbmigrate migrate`

* Performs baseline migration (schema + data).
* Supports `--schema-only` and `--data-only`.
* Safe defaults: refuse to overwrite existing DB unless `--force`.

### `dbmigrate replicate`

* Performs incremental replication only, applying changes since last checkpoint.
* Shows lag estimates and last checkpoint.

### `dbmigrate verify`

* Performs schema and/or data verification without migrating.

### `dbmigrate report`

* Produces machine-readable JSON report (and optional HTML) of diffs, warnings, applied transforms, replication checkpoint, and operator hints.

Global flags (examples):

* `--source "mysql://user:pass@host:3306/?tls=preferred"`
* `--dest "mysql://user:pass@host:3306/?tls=preferred"`
* `--databases "db1,db2"` (default: all non-system DBs)
* `--exclude-databases "information_schema,performance_schema,sys,mysql"`
* `--include-objects "tables,views,routines,triggers,events"` (default all)
* `--concurrency N`
* `--dry-run`
* `--verbose`, `--json`
* `--tls-mode {disabled,preferred,required}`, `--ca-file`, `--cert-file`, `--key-file`
* `--state-dir ./state` (stores checkpoints and metadata)

Migration flags:

* `--consistent-snapshot` (REPEATABLE READ + consistent snapshot when possible)
* `--lock-tables` (fallback)
* `--chunk-size`
* `--resume`
* `--dest-empty-required` (default true)
* `--force`

Replication flags:

* `--replication-mode {binlog,capture-triggers,hybrid}` (default: binlog when supported)
* `--start-from {auto,binlog-file:pos,gtid}` (advanced)
* `--apply-ddl {ignore,apply,warn}` (default: warn+apply safe DDL)
* `--max-events N`
* `--max-lag-seconds N`
* `--conflict-policy {source-wins,dest-wins,fail}` (default: fail)
* `--idempotent` (ensure replays are safe)
* `--enable-trigger-cdc` (required to create CDC triggers/tables)
* `--teardown-cdc` (remove CDC objects cleanly)

Verification flags:

* `--verify-level {schema,data,full}` (default: full)
* `--data-verify {count,hash,sample,full-hash}`
* `--sample-rate 0.01`
* `--hash-alg {xxhash64,sha256}`
* `--row-hash-mode {ordered,pk-ordered}` (default pk-ordered if PK exists)
* `--tolerate-collation-diffs`
* `--ignore-definer-diffs`
* `--ignore-auto-inc`
* `--ignore-table-options`

Exit codes:

* `0` success
* `2` completed but verification found diffs
* `3` migration/replication failed
* `4` verification failed (tool error)

---

## 4) Incremental replication (MUST IMPLEMENT)

## Preferred mode: Binlog-based replication (default when possible)

Implement an incremental replicator that reads source changes via binary logs and applies them to destination.

Requirements:

* Detect source flavor/version and replication capabilities (binlog enabled, format, GTID availability).
* Prefer **ROW** binlog format; detect STATEMENT/MIXED and either refuse or support limited mode with clear warnings.
* Support checkpointing:

  * MySQL: GTID set and/or file+position.
  * MariaDB: MariaDB GTID and/or file+position; explicitly warn that MySQL and MariaDB GTID are not compatible and may require file/pos handling across engines.
* Apply events transactionally, preserving commit boundaries.
* Exactly-once best-effort semantics:

  * Advance checkpoint only after destination commit succeeds.
  * On crash/retry, safely reprocess without double-apply (idempotent apply strategy).
* DDL handling:

  * Parse DDL events and apply if safe and permitted by `--apply-ddl`.
  * For risky DDL (drops, incompatible alters), default to `warn` and stop unless explicitly allowed.
* Schema guard:

  * If a row event cannot be applied due to schema mismatch, stop with actionable message suggesting `migrate --schema-only` or `plan`.

Conflict handling:

* Default `--conflict-policy fail`.
* Provide detailed conflict reports including table, PK, and a small sample of differing values where safe.

Implementation notes (Go):

* Use `database/sql` with a minimal driver (e.g., `github.com/go-sql-driver/mysql`) unless you can justify an even lighter option.
* For binlog streaming, use a well-maintained minimal-dependency Go binlog library; keep it encapsulated behind an interface so it can be swapped. Document why chosen.
* Implement backpressure and bounded memory; avoid unbounded buffering.

## Fallback mode: Trigger-based CDC (only if binlog not possible)

If binlog privileges/settings are unavailable, implement optional trigger-based CDC.

* Requires `--enable-trigger-cdc` to create CDC schema/tables/triggers.
* Create per-table triggers (INSERT/UPDATE/DELETE) writing to CDC log tables with:

  * monotonic sequence id
  * operation
  * primary key values
  * changed columns (or full-row JSON, configurable)
  * source timestamp
* Replicate by reading CDC logs, applying to destination in order, and checkpointing last sequence.
* Provide teardown mode `--teardown-cdc` to remove CDC objects.

## Hybrid mode

* Use binlog generally.
* Allow marking specific tables to use trigger CDC (configurable) when binlog replication is unsafe or unsupported.

---

## 5) Consistency verification (MUST IMPLEMENT)

## Schema verification

Compare source vs destination:

* Databases exist and default charset/collation
* Tables, views
* Columns: type, nullability, default, generated columns, auto_increment, charset/collation
* Indexes: PK/unique/fulltext/spatial, column order, prefix lengths, visibility, index type
* Constraints: FK, check constraints (where supported), ON UPDATE/DELETE actions
* Routines: procedures/functions definitions and metadata
* Triggers: timing/event, body, definers
* Events: definitions and status
* Table options: engine, row_format, partitioning, etc.

Normalization:

* Strip version-specific comments
* Normalize whitespace
* Optionally ignore DEFINER and certain metadata noise
* Canonicalize collations/charsets via mapping rules and document them

## Data verification

Modes:

1. `count`: row counts per table
2. `hash`: chunked hashing with PK ordering when possible
3. `sample`: deterministic sampling
4. `full-hash`: full deterministic table hashing

Row hashing:

* Canonical serialization by column order
* Normalize NULL
* Stable representation for temporal types
* Warn about float precision comparisons (optional tolerance config)

Outputs:

* Human summary
* JSON report with structured diffs and replication checkpoint details

---

## 6) Compatibility & transformation layer (MUST IMPLEMENT)

Detect:

* Flavor/version (MySQL vs MariaDB)
* SQL modes
* default charset/collation
* `lower_case_table_names`
* time zone settings
* binlog format/gtid settings
* engine differences (e.g., JSON behavior/storage differences)
* authentication plugin considerations for MySQL 8+ clients/accounts (feature-flagged)

Transformations (when enabled):

* Strip/normalize `DEFINER`
* Adjust delimiter handling in extracted DDL
* Type/charset/collation mapping (warn by default; allow remap)
* Reserved-word identifier quoting suggestions

If an object cannot be safely translated, fail with a clear error and include remediation in `plan` and `report`.

---

## 7) Architecture & best practices (MANDATORY)

Apply Go best practices and clean architecture:

* Clean separation: CLI, config, DB connectors, extractor, applier, transformer, replicator, verifier, reporter, checkpoint/state manager.
* Dependency injection for DB and binlog components to enable unit tests with mocks/fakes.
* Strict input validation and secure handling of credentials (never log secrets).
* Structured logging (JSON) + human-friendly mode.
* Observability: progress, throughput, lag estimates, error categories.
* Config: CLI flags + optional config file (YAML/JSON), with precedence rules.
* Static analysis and formatting:

  * `gofmt` mandatory
  * `golangci-lint` with a curated minimal set of linters
  * `govulncheck` (or equivalent) in CI
* Release process: versioning, reproducible builds, checksums, SBOM if feasible.

---

## 8) Testing strategy (MUST INCLUDE “ALL KINDS” OF TESTS)

Implement a comprehensive test pyramid:

## Unit tests

* Config parsing/validation
* Normalization and diff logic
* Hashing and canonical serialization
* DDL transformation rules
* Checkpoint manager correctness (crash-safety, idempotency)
* Replication apply logic (mocked events and DB)

## Integration tests (Docker REQUIRED)

Use Docker Compose to spin up:

* MariaDB 10.9
* MariaDB 11.x
* MariaDB 12.x
* MySQL 8.0
* MySQL 8.4

Test harness must:

* Create sample databases with:

  * multiple tables with varied types
  * random data generation (deterministic seed)
  * indexes (PK/unique/fulltext)
  * foreign keys
  * views
  * stored procedures/functions
  * triggers
  * events (scheduler)
  * varied charsets/collations
* Run baseline migration for each scenario and assert verification passes.
* Mutate source:

  * INSERT/UPDATE/DELETE
  * schema change (safe ALTER TABLE)
* Run incremental replication and assert destination matches source (verify full).
* Negative tests:

  * intentional mismatch (drop index, alter trigger, change a row) → verify must detect
  * conflict scenario during replication → must fail with clear report

## End-to-end tests

* Empty destination → fully replicated steady-state
* Run replicate multiple times with no changes: must be no-op and fast
* Crash simulation: interrupt apply mid-run → rerun must resume and remain consistent

## Performance/regression tests

* Large-table simulation with chunking
* Ensure memory usage stays bounded
* Optional benchmark suite with recorded results in CI artifacts

---

## 9) Detailed development plan (MANDATORY)

Provide a detailed plan in `docs/development-plan.md` with:

* Milestones and deliverables
* Task breakdown into small PR-sized units
* For each task: objective, files/modules affected, tests to add, acceptance criteria
* Recommended sequence:

  1. research docs + risk checklist
  2. repo scaffolding + CI + lint/format + PR templates
  3. config system + connection layer
  4. extractor + schema applier baseline
  5. data migrator streaming + checkpoint/resume
  6. schema verifier
  7. data verifier (count/hash/sample/full-hash)
  8. binlog replicator + checkpoint semantics
  9. DDL handling in replication + conflict detection
  10. trigger CDC fallback + teardown
  11. report (JSON/HTML) + UX polish
  12. documentation + examples + hardening
* Explicitly require: “one feature per branch, PR to main, CI green”.

Also include:

* `docs/operators-guide.md`: runbooks (baseline then replicate loop), safe flags, rollback strategy.
* `docs/security.md`: permissions, TLS, secret handling.

---

## 10) Deliverables / repository contents (MANDATORY)

Repo must include:

* `cmd/dbmigrate/` entrypoint
* `internal/` packages for modules described above
* `docs/known-problems.md`, `docs/risk-checklist.md`, `docs/development-plan.md`, `docs/operators-guide.md`, `docs/security.md`
* `.github/workflows/ci.yml`
* `docker-compose.yml` for integration tests
* `tests/` (unit + integration harness)
* `README.md` with quickstart, examples, limitations, and safety warnings
* `LICENSE` (permissive unless told otherwise)
* PR template in `.github/pull_request_template.md`

---

## 11) Acceptance criteria

The project is accepted when:

* Baseline migration works reliably across the three scenario families.
* Incremental replication can run repeatedly and applies only changes since last successful run (checkpointed), with crash-safe behavior.
* Verification detects missing objects and data mismatches.
* Tool surfaces known migration pitfalls with preflight warnings and documented mitigations based on the research.
* Tests are comprehensive and automated in CI with Docker Compose.
* Git/GitHub best practices are followed (branch-per-feature, PRs, CI gates, docs).

Now implement the full project accordingly, including the complete repository structure, Go code, tests, Docker, CI, and documentation, and ensure the development plan and PR workflow instructions are explicit and actionable.
