# dbmigrate v1 Release Decision

## Decision

**Release approved with explicit caveats.**

This decision applies to the implemented and genuinely supported self-managed `v1` paths only.

It does not approve:

- reserved `v2` paths
- managed/cloud `v3` paths
- non-frozen supplemental paths as part of the strict-lts release lane

## Release candidate scope

Approved `v1` scope is limited to:

- baseline migration
- binlog replication mode
- schema/data verification
- report command
- merged fail-fast prechecks and operator-safety hardening
- self-managed deployments only
- frozen exact support matrix defined in `README.md`

The following remain outside `v1`:

- `--replication-mode=capture-triggers`
- `--replication-mode=hybrid`
- `--start-from=gtid`
- trigger-CDC lifecycle flags
- managed/cloud deployment support

## Evidence used

Primary evidence inputs:

- [README.md](/Volumes/UserData/Works/dbmigrate/README.md)
- [docs/v1-release-plan.md](/Volumes/UserData/Works/dbmigrate/docs/v1-release-plan.md)
- [docs/v1-release-criteria.md](/Volumes/UserData/Works/dbmigrate/docs/v1-release-criteria.md)
- [docs/v1-matrix-evidence.md](/Volumes/UserData/Works/dbmigrate/docs/v1-matrix-evidence.md)
- [docs/v1-rehearsal-evidence.md](/Volumes/UserData/Works/dbmigrate/docs/v1-rehearsal-evidence.md)

Local release-decision verification on this branch:

```bash
go build -trimpath -ldflags='-s -w' -o bin/dbmigrate ./cmd/dbmigrate
go test ./...
```

Code-bearing evidence baseline:

- frozen-matrix evidence revision: `297e989`
- focused-rehearsal evidence revision: `0bf79f4`
- current release-decision branch base before this doc: `e409a90`

The decision is valid because the changes after the evidence revisions were documentation/signoff changes, not product-behavior changes.

## Criteria judgment

### A) Scope

- [x] Frozen `v1` matrix approved
- [x] Unsupported paths explicitly excluded from `v1`
- [x] Reserved `v2` and `v3` paths clearly labeled

Judgment:

- `v1` scope is now explicit and narrow enough to defend.
- The public support surface no longer depends on implied future functionality.

### B) Build and CI

- [x] `go build ...` passed locally on this branch
- [x] `go test ./...` passed locally on this branch
- [x] Required CI checks were green on the merged evidence branches and must remain green for the final PR

Judgment:

- local build/test gate is satisfied
- final approval remains contingent on the final signoff PR checks staying green

### C) Matrix

- [x] Fresh full `v1` matrix completed
- [x] Every supported frozen path passed
- [x] No frozen path failed unexpectedly
- [x] No scenario remains unclassified

Judgment:

- frozen strict-lts lane: green
- supplemental lane: green, but not part of strict-lts signoff

Frozen strict-lts results confirmed:

- `MySQL 8.4 -> MySQL 8.4`
- `MariaDB 10.11 -> MariaDB 10.11`
- `MariaDB 11.4 -> MariaDB 11.4`
- `MariaDB 11.8 -> MariaDB 11.8`
- `MySQL 8.4 -> MariaDB 11.4`
- `MariaDB 11.4 -> MySQL 8.4`

### D) Focused rehearsals

- [x] Metadata-lock rehearsal rerun and archived
- [x] Backup/restore rehearsal rerun and archived
- [x] Time-zone rehearsal rerun and archived
- [x] Plugin lifecycle rehearsal rerun and archived
- [x] Replication transaction-shape rehearsal rerun and archived
- [x] Invisible/GIPK rehearsal rerun and archived
- [x] Collation rehearsal rerun and archived
- [x] Verify canonicalization rehearsal rerun and archived

Judgment:

- focused rehearsal pack is green
- archived root: `state/v1-signoff-rehearsals/20260307T003408Z`
- expected fail-fast paths failed fast with clear evidence
- warning-only paths remained non-blocking
- no unexpected contradiction with operator docs was found

### E) Documentation

- [x] `README.md` matches `v1`
- [x] Operators guide matches `v1`
- [x] Risk checklist matches `v1`
- [x] Known-problems doc matches `v1`
- [x] Release plan and signoff docs match the actual release process

Judgment:

- support surface and release process documentation are aligned
- no stale “candidate as supported” wording remains in the `v1` surface

### F) Final decision

- [x] No unresolved blocker remains for a frozen `v1` path
- [x] Release report is attached through tracked evidence docs
- [ ] Release owner signs off

Judgment:

- technical signoff is ready
- human release-owner signoff is still the final non-technical step

## Explicit caveats

`v1` is approved with these explicit caveats:

1. Supported cross-engine strict-lts paths still emit warning-only findings for auth-plugin drift.
   - This is acceptable for `v1` because account execution is not part of baseline schema compatibility.
   - Operators must review the report output before cutover.

2. MariaDB `11.4` and `11.8` paths can emit warning-only `uca1400` client-compatibility risk.
   - Server-side migration and verification passed.
   - The caveat is about application/client stack compatibility, not server-side schema application.

3. Supplemental upgrade evidence is not part of the frozen strict-lts release promise.
   - It is useful additional evidence, not part of the narrow `v1` support guarantee.

4. Reserved modes remain out of scope.
   - `v2` and `v3` work is not implicitly approved by this document.

## Blocking conditions checked and cleared

The release remains **not blocked** on the frozen `v1` lane because there is no evidence of:

- unexpected `plan` incompatibility on a frozen path
- unexpected `migrate` failure on a frozen path
- unexpected binlog replication failure under documented prerequisites
- unexplained `verify` diffs on a frozen path
- contradictory `report` semantics
- documentation drift that changes the supported surface

## Final read

Current release read:

- frozen strict-lts matrix: green
- focused rehearsals: green
- docs/support surface: aligned
- local build/test gate: green
- release posture: **approved with explicit caveats**

## Required final action

For the release to be fully closed:

1. this decision PR must merge with green CI
2. the release owner must mark the signoff step complete

Until then, the technical decision is ready but not yet fully ratified.
