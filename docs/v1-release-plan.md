# dbmigrate v1 Release Plan

## Objective

Deliver a production-ready `v1` for paths that are already implemented and genuinely supportable today.

`v1` is a release-hardening program, not a new feature program.

Success means:

- the supported `v1` matrix is explicit and frozen
- public docs match the actual product surface
- a fresh full matrix run on current `main` validates every supported `v1` path
- unsupported or reserved paths are labeled clearly enough that operators cannot confuse them with release-grade support

## Product scope

### In scope for `v1`

- self-managed deployments only
- baseline schema migration
- baseline data migration with checkpoint/resume
- replication in implemented binlog mode
- schema verification
- data verification:
  - `count`
  - `hash`
  - `sample`
  - `full-hash`
- report generation from current state artifacts
- fail-fast compatibility prechecks already merged into `main`

### Out of scope for `v1`

Reserved for `v2`:

- `--replication-mode=capture-triggers`
- `--replication-mode=hybrid`
- `--enable-trigger-cdc`
- `--teardown-cdc`
- `--start-from=gtid`

Reserved for `v3`:

- managed/cloud deployment support
- provider-specific qualification for RDS, Cloud SQL, Azure Database for MySQL, Aurora, and similar platforms
- managed failover/durability validation as a release-grade promise

### Scope rule

If a path is not implemented and validated, it is not part of `v1`, even if the CLI already reserves a flag or mode name for it.

## v1 support policy

### Supported

Only exact engine/version pairs validated by the frozen `v1` matrix and backed by a fresh full release run.

### Supported with caution

Paths exposed through `max-compat` may remain available for operator-reviewed use, but they are not release-grade `v1` support unless they are explicitly promoted into the frozen matrix.

### Unsupported

- any pair outside the frozen `v1` matrix
- any path blocked by current prechecks as incompatible by design
- any reserved replication mode not implemented in code

## Required public-surface cleanup

Before release, public docs must stop implying broader support than the product actually has.

Required changes:

1. Update `README.md` to reflect the current merged baseline, not older phase counts.
2. Add explicit sections:
   - `Supported in v1`
   - `Not supported in v1`
   - `Reserved for v2`
   - `Reserved for v3`
3. Align command examples and wording so reserved paths are clearly marked as non-v1.
4. Ensure downgrade-profile language distinguishes:
   - strict validated support
   - operator-reviewed but non-release-guaranteed compatibility
   - candidate paths not yet promoted

## Frozen v1 matrix

The release owner must freeze the exact supported matrix before final validation.

This matrix must define:

- engine pair
- exact version pair or version line
- downgrade profile required
- required source settings
- required destination settings
- whether the path is same-engine or cross-engine
- required verification mode for release signoff

The release should not proceed with ambiguous labels such as "candidate", "likely supported", or "best effort" inside the actual `v1 supported` section.

## Release gates

Every `v1` release candidate must satisfy all gates below.

### Gate 1: Scope gate

- `README.md` and operator docs match the real `v1` support promise.
- Reserved `v2` and `v3` paths are clearly excluded from `v1`.

### Gate 2: Build and unit gate

- `go build -trimpath -ldflags='-s -w' -o bin/dbmigrate ./cmd/dbmigrate`
- `go test ./...`
- CI validation green on the release candidate branch

### Gate 3: Matrix gate

Run a fresh full matrix on current `main` or the release candidate branch for all frozen `v1` pairs.

Each scenario must record:

- source service and exact version
- destination service and exact version
- selected downgrade profile
- `plan` result
- `migrate` result
- `verify` result
- `report` result
- whether failure was:
  - expected unsupported-by-design
  - unexpected regression

### Gate 4: Rehearsal gate

Re-run focused rehearsals that protect the highest-risk operator claims now present in `v1`:

- metadata lock observability
- backup/restore rehearsal
- time-zone and `NOW()` semantics
- plugin lifecycle and unsupported engine detection
- replication transaction-shape diagnostics
- invisible-column and GIPK downgrade evidence
- collation compatibility and client-risk separation
- verify canonicalization false-positive control

### Gate 5: Report gate

Generate one consolidated release-grade compatibility report that states:

- supported `v1` paths confirmed by fresh evidence
- blocked paths that are unsupported by design
- warnings or caveats that still apply within supported paths
- any unresolved issues that would block release

### Gate 6: Documentation gate

These must all be synchronized with the frozen `v1` surface:

- `README.md`
- `docs/operators-guide.md`
- `docs/risk-checklist.md`
- `docs/known-problems.md`
- any release report summary produced for the matrix run

## Execution plan

### PR 1: v1 support-surface cleanup

Objective:

- make the public product surface match the actual `v1` scope

Expected changes:

- update `README.md`
- add explicit `v1/v2/v3` scope language
- mark reserved replication paths as non-v1
- freeze wording around supported versus candidate compatibility pairs

Acceptance criteria:

- no public doc implies `capture-triggers`, `hybrid`, GTID start, or managed/cloud support are part of `v1`
- the supported/unsupported boundary is readable without opening source code

### PR 2: v1 release criteria and signoff doc

Objective:

- codify what evidence is required to call a build release-ready

Expected changes:

- add or finalize a release checklist document
- define matrix evidence requirements
- define verify/report acceptance criteria
- define rollback expectations and minimum preflight expectations

Acceptance criteria:

- release signoff can be executed from docs without oral tradition

### PR 3: fresh full `v1` matrix execution and consolidated report

Objective:

- produce current, release-grade evidence on the exact frozen `v1` matrix

Expected changes:

- matrix run artifacts
- consolidated compatibility report
- concise summary of supported paths, blocked-by-design paths, and regressions

Acceptance criteria:

- every frozen `v1` path has fresh evidence
- every failure is classified as expected or unexpected

### PR 4+ : targeted fixes for regressions found by the matrix

Objective:

- repair any release-blocking failures without bundling unrelated changes

Expected changes:

- one focused PR per issue family where possible
- regression tests
- rerun evidence for affected scenarios

Acceptance criteria:

- each unexpected matrix failure is either fixed or explicitly re-scoped out of `v1`

### Final PR: v1 readiness finalization

Objective:

- lock the release evidence and public docs together

Expected changes:

- final doc synchronization
- final report references
- explicit statement of the frozen `v1` support matrix

Acceptance criteria:

- `main` is green
- release docs match the final evidence
- no unresolved blocker remains for a frozen `v1` path

## Matrix execution rules

When the full `v1` matrix is run:

1. use current `main` or the release candidate branch only
2. keep one output directory per release run
3. preserve exact command lines used
4. preserve exact service versions used
5. separate:
   - supported and passed
   - unsupported by design
   - failed unexpectedly

This is the minimum needed to defend a production-readiness claim.

## Regressions and stop conditions

Release work must stop and loop back to fixes if any of the following occur on a frozen `v1` path:

- `plan` gives an unexpected incompatibility
- `migrate` fails unexpectedly
- `replicate` in binlog mode fails under supported prerequisites
- `verify` produces unexplained diffs
- `report` semantics contradict the actual artifact state
- rollback or restore evidence is missing for the claimed workflow

## Recommended release stance

Use this release stance unless new evidence justifies something stronger:

- `strict-lts` plus exact validated pairs = release-grade `v1` support
- `max-compat` = available with warnings, but not release-grade unless explicitly promoted
- reserved replication modes = `v2`
- managed/cloud = `v3`

This keeps `v1` honest and defensible.

## Immediate next step

Start with `README` and support-surface cleanup before spending time on a fresh full matrix run.

Reason:

- running a huge validation pass against an ambiguous support promise is wasted effort
- public scope must be frozen before evidence can mean anything
