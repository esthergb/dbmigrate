# dbmigrate v1 Focused Rehearsal Evidence

## Purpose

This document records the focused rehearsal rerun required by [docs/v1-release-criteria.md](/Volumes/UserData/Works/dbmigrate/docs/v1-release-criteria.md) for `v1` signoff.

It is not the frozen matrix report.
It is the operator-risk evidence pack that complements the matrix report.

## Tested revision

- git revision: `0bf79f4`
- branch used for archival: `codex/chore/v1-signoff-rehearsals`

## Archived output root

- output root: `state/v1-signoff-rehearsals/20260307T003408Z`
- manifest: `state/v1-signoff-rehearsals/20260307T003408Z/manifest.tsv`
- top summary: `state/v1-signoff-rehearsals/20260307T003408Z/summary.json`

This run supersedes the earlier partial root `state/v1-signoff-rehearsals/20260307T003054Z`, which captured valid evidence but exposed a collation-wrapper archival bug before the top-level summary was written.

## Service-targeting decision

For signoff consistency, rehearsals were retargeted to the frozen `v1` lane wherever that improved release relevance:

- retargeted to frozen `v1` services:
  - metadata-lock
  - backup/restore
  - time-zone and `NOW()`
  - plugin lifecycle
  - replication transaction shape
  - invisible-column and GIPK
  - verify canonicalization
- collation rehearsal:
  - server-side scenarios were retargeted to `v1`-relevant services
  - the representative client probe intentionally stayed on `mysql80a`
    - reason: it is a client-compatibility probe, not a supported `v1` server path

## Commands run

```bash
go test ./...
./scripts/run-v1-signoff-rehearsals.sh
```

## Pack result

- orchestrator `failed_steps`: `0`
- archived steps: `14`
- unexpected failures: `0`

## Rehearsal classification

### Expected operator-signal rehearsals

These are successful when the expected stress or representation signal appears and the wrapper still archives clean evidence.

1. `metadata_lock_mysql84a`
   - classification: expected operator-signal pass
   - evidence:
     - `ddl_exit_code=1`
     - `read_exit_code=0`
     - `queue_amplification_detected=true`
     - `metadata_locks_available=true`

2. `metadata_lock_mariadb114a`
   - classification: expected operator-signal pass
   - evidence:
     - `ddl_exit_code=1`
     - `read_exit_code=0`
     - `queue_amplification_detected=true`
     - `metadata_locks_available=true`

3. `backup_restore_mysql84a`
   - classification: expected recovery-evidence pass
   - evidence:
     - `backup_completed=true`
     - `backup_validated=true`
     - `restore_usable=true`

4. `backup_restore_mariadb114a`
   - classification: expected recovery-evidence pass
   - evidence:
     - `backup_completed=true`
     - `backup_validated=true`
     - `restore_usable=true`

5. `timezone_mysql84a`
   - classification: expected semantics-evidence pass
   - evidence:
     - `timestamp_display_changes=true`
     - `datetime_static_under_session_change=true`
     - `explicit_now_drift_visible=true`

6. `timezone_mariadb114a`
   - classification: expected semantics-evidence pass
   - evidence:
     - `timestamp_display_changes=true`
     - `datetime_static_under_session_change=true`
     - `explicit_now_drift_visible=true`

7. `replication_shape_mysql84a`
   - classification: expected transaction-shape signal pass
   - evidence:
     - `monolithic.transaction_count=1`
     - `chunked.transaction_count=20`
     - `same_total_rows=true`
     - `monolithic_dominates_transaction_shape=true`
     - `chunked_reduces_commit_granularity=true`

8. `replication_shape_mariadb114a`
   - classification: expected transaction-shape signal pass
   - evidence:
     - `monolithic.transaction_count=1`
     - `chunked.transaction_count=20`
     - `same_total_rows=true`
     - `monolithic_dominates_transaction_shape=true`
     - `chunked_reduces_commit_granularity=true`

9. `verify_canonicalization_phase64`
   - classification: expected false-positive-control pass
   - evidence:
     - `naive_hashes_differ=true`
     - `verify_hash_exit_code=0`
     - `verify_sample_exit_code=0`
     - `verify_full_hash_exit_code=0`
     - `noise_risk_mismatches=0`

### Expected compatible paths

These are successful when the scenario remains usable on the intended path.

1. `plugin_lifecycle_mysql84a_to_mariadb114b`
   - classification: compatible-with-warnings pass
   - evidence:
     - `plan_exit_code=0`
     - `unsupported_auth_plugins_detected=true`
     - `unsupported_storage_engines_detected=false`

2. `invisible_gipk_mysql84a_to_mysql84b`
   - classification: compatible hidden-schema preservation pass
   - evidence:
     - `plan_exit_code=0`
     - `included_invisible_column_preserved=true`
     - `included_invisible_index_preserved=true`
     - `included_gipk_remains_invisible=true`
     - `visibility_drift_detected=false`

3. `collation_phase63` client-risk sub-scenario:
   - sub-summary: `state/v1-signoff-rehearsals/20260307T003408Z/collation/mariadb114a-to-mariadb114b-uca1400/summary.json`
   - classification: warning-only client-risk pass
   - evidence:
     - `plan_exit_code=0`
     - `report_exit_code=0`
     - `restore_exit_code=0`
     - `unsupported_destination_count=0`
     - `client_compatibility_risk_count=7`

### Expected fail-fast evidence

These are successful when the tool rejects or exposes the incompatible path clearly.

1. `plugin_lifecycle_mariadb114a_to_mysql84b`
   - classification: expected fail-fast evidence pass
   - evidence:
     - `plan_exit_code=2`
     - `unsupported_auth_plugins_detected=true`
     - `unsupported_storage_engines_detected=true`

2. `invisible_gipk_mysql84a_to_mariadb114b`
   - classification: expected fail-fast evidence pass
   - evidence:
     - `plan_exit_code=2`
     - `included_invisible_column_preserved=false`
     - `included_invisible_index_preserved=false`
     - `included_gipk_remains_invisible=false`
     - `visibility_drift_detected=true`

3. `collation_phase63` unsupported `utf8mb4_0900_ai_ci` sub-scenario:
   - sub-summary: `state/v1-signoff-rehearsals/20260307T003408Z/collation/mysql84a-to-mariadb1011a-0900/summary.json`
   - classification: expected fail-fast evidence pass
   - evidence:
     - `plan_exit_code=2`
     - `report_exit_code=2`
     - `restore_exit_code=1`
     - `unsupported_destination_count=5`
     - `representative_client_probe_exit_code=0`

4. `collation_phase63` unsupported `utf8mb4_uca1400_ai_ci` to MySQL sub-scenario:
   - sub-summary: `state/v1-signoff-rehearsals/20260307T003408Z/collation/mariadb114a-to-mysql84b-uca1400/summary.json`
   - classification: expected fail-fast evidence pass
   - evidence:
     - `plan_exit_code=2`
     - `report_exit_code=2`
     - `restore_exit_code=1`
     - `unsupported_destination_count=5`
     - `client_compatibility_risk_count=6`
     - `representative_client_probe_exit_code=0`

## Conclusion

The focused rehearsal pack is green for `v1` signoff purposes:

- the orchestration layer archived every required rehearsal
- expected compatible paths remained compatible
- expected fail-fast paths failed fast with clear evidence
- warning-only paths remained non-blocking
- no unexpected failure or contradictory operator signal appeared in this run

This satisfies the focused-rehearsal evidence requirement of `docs/v1-release-criteria.md`, subject to final release-bundle assembly and signoff.
