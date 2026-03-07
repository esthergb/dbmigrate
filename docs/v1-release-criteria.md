# dbmigrate v1 Release Criteria

## Purpose

This document is the signoff gate for `dbmigrate v1`.

Use it to decide whether a specific `main` revision is ready to be called a production-ready `v1` for the frozen supported paths.

It is not a roadmap document.
It is a release decision document.

## Scope

This signoff applies only to `v1` scope:

- implemented paths only
- self-managed deployments only
- exact supported engine/version pairs frozen for the release

This signoff does not cover:

- reserved `v2` paths
- managed/cloud `v3` paths
- candidate compatibility pairs not yet promoted into the frozen matrix

## Required inputs

The release owner must have all of the following before signoff starts:

1. frozen `v1` support matrix
2. current `README.md` aligned to the same support surface
3. current operator docs aligned to the same support surface
4. fresh matrix execution evidence on current `main` or the release candidate branch
5. focused rehearsal evidence for the highest-risk operator claims

If any input is missing, signoff does not begin.

## Frozen matrix requirements

The frozen `v1` matrix must define, for each supported path:

- source engine
- source version line
- destination engine
- destination version line
- allowed downgrade profile
- required source settings
- required destination settings
- required verification mode
- expected operator caveats

Every supported path must be exact.

Examples of unacceptable matrix language:

- "likely supported"
- "best effort"
- "candidate"
- "probably works"

Those are planning terms, not release terms.

## Build and CI gate

The release candidate must satisfy all of the following:

- `go build -trimpath -ldflags='-s -w' -o bin/dbmigrate ./cmd/dbmigrate`
- `go test ./...`
- required CI checks green

If any one fails, release is blocked.

Recommended local gate runner:

- `./scripts/run-v1-release-gate.sh --mode minimal` for fast pre-PR validation
- `./scripts/run-v1-release-gate.sh --mode full` for release-manager signoff runs

Optional GitHub-hosted manual runner:

- `v1-release-gate` workflow (`.github/workflows/v1-release-gate.yml`)
  - input `mode=minimal|full`
  - uploads `state/v1-release-gate/...` artifacts for audit retention

## Matrix gate

Run the full frozen `v1` matrix on current `main` or the release candidate branch.

For each scenario, record:

- scenario name
- source service name and exact version
- destination service name and exact version
- profile used
- `plan` exit code
- `migrate` exit code
- `verify` exit code
- `report` exit code
- classification:
  - supported and passed
  - unsupported by design
  - unexpected regression

### Matrix acceptance rule

Release is blocked if any frozen `v1` path is classified as `unexpected regression`.

Release is also blocked if any path cannot be classified cleanly.

## Focused rehearsal gate

The following focused rehearsals must be rerun on release-grade code and archived with their output locations:

- metadata-lock rehearsal
- backup/restore rehearsal
- time-zone and `NOW()` rehearsal
- plugin lifecycle rehearsal
- replication transaction-shape rehearsal
- invisible-column and GIPK rehearsal
- collation rehearsal
- verify canonicalization rehearsal

### Rehearsal acceptance rule

Release is blocked if:

- a rehearsal fails unexpectedly
- a rehearsal cannot be reproduced
- the recorded outcome contradicts the current operator docs

## Documentation gate

The following docs must match the frozen `v1` support surface:

- `README.md`
- `docs/operators-guide.md`
- `docs/risk-checklist.md`
- `docs/known-problems.md`
- `docs/v1-release-plan.md`
- this file

### Documentation acceptance rule

Release is blocked if documentation:

- describes unsupported paths as if they were part of `v1`
- hides required prerequisites or caveats for supported paths
- disagrees with current command behavior

## Report and verification gate

For supported `v1` paths:

- `verify` behavior must match the documented semantics
- `report` behavior must match the documented semantics
- exit codes must match the documented semantics

Specific expectations:

- incompatible precheck artifacts produce `attention_required`
- unresolved active replication conflicts produce `attention_required`
- warning-only artifacts remain non-blocking unless explicitly documented otherwise
- verify artifacts distinguish real diffs from representation-sensitive evidence

### Report/verify acceptance rule

Release is blocked if:

- `verify` produces unexplained diffs on a supported path
- `report` status does not match the underlying artifact reality
- exit codes diverge from the documented contract

## Rollback and recovery gate

For the intended `v1` operator workflow:

- rollback expectations must be documented
- backup/restore rehearsal evidence must be available
- restart/resume expectations must be documented for baseline and replication workflows

### Rollback acceptance rule

Release is blocked if rollback claims exist without evidence for the documented workflow.

## Stop conditions

Release work must stop and return to a fix cycle if any of the following occur:

- unexpected `plan` incompatibility on a frozen `v1` path
- unexpected `migrate` failure on a frozen `v1` path
- unexpected binlog replication failure under documented prerequisites
- unexplained `verify` diffs on a frozen `v1` path
- contradictory `report` semantics
- documentation/support-surface drift discovered during signoff

## Signoff checklist

### A) Scope

- [ ] Frozen `v1` matrix approved
- [ ] Unsupported paths explicitly excluded from `v1`
- [ ] Reserved `v2` and `v3` paths clearly labeled

### B) Build and CI

- [ ] `go build ...` passed
- [ ] `go test ./...` passed
- [ ] CI checks passed

### C) Matrix

- [ ] Fresh full `v1` matrix completed
- [ ] Every supported path passed
- [ ] Every unsupported path was blocked by design, not by surprise
- [ ] No scenario remains unclassified

### D) Focused rehearsals

- [ ] Metadata-lock rehearsal rerun and archived
- [ ] Backup/restore rehearsal rerun and archived
- [ ] Time-zone rehearsal rerun and archived
- [ ] Plugin lifecycle rehearsal rerun and archived
- [ ] Replication transaction-shape rehearsal rerun and archived
- [ ] Invisible/GIPK rehearsal rerun and archived
- [ ] Collation rehearsal rerun and archived
- [ ] Verify canonicalization rehearsal rerun and archived

### E) Docs

- [ ] `README.md` matches `v1`
- [ ] Operators guide matches `v1`
- [ ] Risk checklist matches `v1`
- [ ] Known-problems doc matches `v1`
- [ ] Release plan and signoff docs match actual release process

### F) Final decision

- [ ] No unresolved blocker remains for a frozen `v1` path
- [ ] Release report is attached
- [ ] Release owner signs off

## Required outputs from signoff

The signoff process must produce:

1. one release-grade matrix summary
2. one release-grade compatibility report
3. links or paths to focused rehearsal outputs
4. a signed decision:
   - release approved
   - release blocked
   - release approved with explicit caveats

## Decision rule

`v1` is approved only when supported paths are proven, unsupported paths are explicit, and operator documentation matches reality.

Anything less is still staging, not release.
