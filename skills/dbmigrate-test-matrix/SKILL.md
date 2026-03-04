---
name: dbmigrate-test-matrix
description: Run and triage dbmigrate validation across minimal CI smoke tests and full local Docker matrices on Apple Silicon for MariaDB/MySQL combinations. Use when verifying milestone readiness, reproducing compatibility bugs, or preparing release-quality validation evidence.
---

# dbmigrate test matrix

## Test strategy

- Treat CI as fast guardrail: lint + unit + smoke integration only.
- Treat local as exhaustive validation: full cross-version matrix and failure triage.

## Workflow

1. Run smoke checks using `scripts/run_smoke.sh`.
2. If smoke is green, run full matrix using `scripts/run_full_matrix.sh`.
3. Capture failures with exact source/destination flavor-version pairing.
4. Categorize failures as:
   - unsupported scenario,
   - known compatibility issue,
   - regression in dbmigrate logic,
   - environment issue.
5. Attach concise evidence to milestone notes.

## Apple Silicon constraints

- Prefer images that provide arm64 manifests.
- Pin image tags known to work on arm64.
- Fail fast with clear message when architecture/image mismatch occurs.

## Reporting expectations

- Always report command set executed.
- Include failing pair(s), error signature, and suspected subsystem.
- Provide rerun command for reproducibility.
