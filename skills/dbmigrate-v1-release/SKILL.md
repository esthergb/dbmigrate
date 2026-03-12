---
name: dbmigrate-v1-release
description: Execute v1 release gate validation, signoff checklist, and tag preparation. Use when running the final v1 release process including matrix execution, focused rehearsals, documentation synchronization, and release decision.
---

# dbmigrate v1 release

## Objective

Validate that current `main` satisfies all v1 release gates and produce signoff evidence.

## Prerequisites

Before starting, verify these exist:
- `docs/v1-release-criteria.md` — signoff gate definitions.
- `docs/v1-release-plan.md` — execution plan and scope.
- `docs/v1-release-decision.md` — decision record.
- `docs/v1-matrix-evidence.md` — matrix run results.
- `docs/v1-rehearsal-evidence.md` — focused rehearsal outputs.

## Workflow

1. Verify build and CI gate:
   - `go build -trimpath -ldflags='-s -w' -o bin/dbmigrate ./cmd/dbmigrate`
   - `go test ./... -count=1`
   - `go vet ./...`
   - Confirm CI is green on `main`.

2. Execute frozen v1 matrix (see `skills/dbmigrate-test-matrix/references/matrix.md`):
   - Run all strict-lts pairs via `scripts/test-v1-*.sh`.
   - Record per-pair: plan/migrate/verify/report exit codes.
   - Classify each as: supported-and-passed, unsupported-by-design, or unexpected-regression.
   - Block release on any unexpected regression.

3. Execute focused rehearsals:
   - Metadata-lock rehearsal.
   - Backup/restore rehearsal.
   - Timezone and `NOW()` rehearsal.
   - Plugin lifecycle rehearsal.
   - Replication transaction-shape rehearsal.
   - Invisible-column and GIPK rehearsal.
   - Collation rehearsal.
   - Verify canonicalization rehearsal.
   - Archive all outputs in `docs/v1-rehearsal-evidence.md`.

4. Documentation gate:
   - Verify `README.md` matches frozen v1 surface.
   - Verify `docs/operators-guide.md` matches frozen v1 surface.
   - Verify reserved v2/v3 paths are clearly excluded.
   - Verify exit code documentation matches implementation.

5. Report gate:
   - Run `report` on a completed migration and verify status semantics.
   - Confirm incompatible precheck artifacts produce `attention_required`.
   - Confirm verify artifacts distinguish real diffs from representation noise.

6. Produce signoff:
   - Update `docs/v1-release-decision.md` with decision (approved/blocked/caveats).
   - Update `docs/v1-matrix-evidence.md` with fresh results.
   - Tag `v1.0.0` only after all gates pass and user confirms.

## Stop conditions

Return to fix cycle if:
- Any frozen v1 path fails unexpectedly.
- Any rehearsal contradicts operator docs.
- Documentation/support-surface drift discovered.
- `verify` produces unexplained diffs on a supported path.
- `report` status does not match underlying artifacts.
