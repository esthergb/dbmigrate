#!/usr/bin/env bash
set -euo pipefail

OUTPUT_ROOT="${1:-./state/v1-signoff-rehearsals/$(date -u +%Y%m%dT%H%M%SZ)}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
OUTPUT_ROOT_ABS="$PROJECT_ROOT/${OUTPUT_ROOT#./}"
MANIFEST_TSV="$OUTPUT_ROOT_ABS/manifest.tsv"
SUMMARY_JSON="$OUTPUT_ROOT_ABS/summary.json"

compose() {
  docker compose -f "$PROJECT_ROOT/docker-compose.yml" "$@"
}

json_escape() {
  local s="$1"
  s="${s//\\/\\\\}"
  s="${s//\"/\\\"}"
  s="${s//$'\n'/\\n}"
  s="${s//$'\r'/\\r}"
  s="${s//$'\t'/\\t}"
  printf '%s' "$s"
}

run_step() {
  local step_name="$1"
  local summary_path="$2"
  shift 2
  local started_at ended_at rc
  started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  set +e
  "$@"
  rc=$?
  set -e
  ended_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf '%s\t%s\t%s\t%s\n' "$step_name" "$rc" "$summary_path" "$ended_at" >>"$MANIFEST_TSV"
  if [ "$rc" -ne 0 ]; then
    echo "step failed: $step_name (rc=$rc)" >&2
  fi
  return 0
}

mkdir -p "$OUTPUT_ROOT_ABS"
printf 'step\trc\tsummary\tcompleted_at_utc\n' >"$MANIFEST_TSV"

SERVICES=(mysql80a mysql84a mysql84b mariadb1011a mariadb114a mariadb114b)

echo "Starting signoff rehearsal services..."
compose up -d "${SERVICES[@]}"

run_step "metadata_lock_mysql84a" "$OUTPUT_ROOT_ABS/metadata-lock/mysql84a/summary.json" \
  "$SCRIPT_DIR/run-metadata-lock-scenario.sh" mysql84a "$OUTPUT_ROOT_ABS/metadata-lock/mysql84a"
run_step "metadata_lock_mariadb114a" "$OUTPUT_ROOT_ABS/metadata-lock/mariadb114a/summary.json" \
  "$SCRIPT_DIR/run-metadata-lock-scenario.sh" mariadb114a "$OUTPUT_ROOT_ABS/metadata-lock/mariadb114a"

run_step "backup_restore_mysql84a" "$OUTPUT_ROOT_ABS/backup-restore/mysql84a/summary.json" \
  "$SCRIPT_DIR/run-backup-restore-rehearsal.sh" mysql84a "$OUTPUT_ROOT_ABS/backup-restore/mysql84a"
run_step "backup_restore_mariadb114a" "$OUTPUT_ROOT_ABS/backup-restore/mariadb114a/summary.json" \
  "$SCRIPT_DIR/run-backup-restore-rehearsal.sh" mariadb114a "$OUTPUT_ROOT_ABS/backup-restore/mariadb114a"

run_step "timezone_mysql84a" "$OUTPUT_ROOT_ABS/timezone/mysql84a/summary.json" \
  "$SCRIPT_DIR/run-timezone-rehearsal.sh" mysql84a "$OUTPUT_ROOT_ABS/timezone/mysql84a"
run_step "timezone_mariadb114a" "$OUTPUT_ROOT_ABS/timezone/mariadb114a/summary.json" \
  "$SCRIPT_DIR/run-timezone-rehearsal.sh" mariadb114a "$OUTPUT_ROOT_ABS/timezone/mariadb114a"

run_step "plugin_lifecycle_mysql84a_to_mariadb114b" "$OUTPUT_ROOT_ABS/plugin-lifecycle/mysql84a-to-mariadb114b/summary.json" \
  "$SCRIPT_DIR/run-plugin-lifecycle-rehearsal.sh" mysql84a mariadb114b "$OUTPUT_ROOT_ABS/plugin-lifecycle/mysql84a-to-mariadb114b"
run_step "plugin_lifecycle_mariadb114a_to_mysql84b" "$OUTPUT_ROOT_ABS/plugin-lifecycle/mariadb114a-to-mysql84b/summary.json" \
  "$SCRIPT_DIR/run-plugin-lifecycle-rehearsal.sh" mariadb114a mysql84b "$OUTPUT_ROOT_ABS/plugin-lifecycle/mariadb114a-to-mysql84b"

run_step "replication_shape_mysql84a" "$OUTPUT_ROOT_ABS/replication-shape/mysql84a/summary.json" \
  "$SCRIPT_DIR/run-replication-shape-rehearsal.sh" mysql84a "$OUTPUT_ROOT_ABS/replication-shape/mysql84a"
run_step "replication_shape_mariadb114a" "$OUTPUT_ROOT_ABS/replication-shape/mariadb114a/summary.json" \
  "$SCRIPT_DIR/run-replication-shape-rehearsal.sh" mariadb114a "$OUTPUT_ROOT_ABS/replication-shape/mariadb114a"

run_step "invisible_gipk_mysql84a_to_mysql84b" "$OUTPUT_ROOT_ABS/invisible-gipk/mysql84a-to-mysql84b/summary.json" \
  "$SCRIPT_DIR/run-invisible-gipk-rehearsal.sh" mysql84a mysql84b "$OUTPUT_ROOT_ABS/invisible-gipk/mysql84a-to-mysql84b"
run_step "invisible_gipk_mysql84a_to_mariadb114b" "$OUTPUT_ROOT_ABS/invisible-gipk/mysql84a-to-mariadb114b/summary.json" \
  "$SCRIPT_DIR/run-invisible-gipk-rehearsal.sh" mysql84a mariadb114b "$OUTPUT_ROOT_ABS/invisible-gipk/mysql84a-to-mariadb114b"

run_step "collation_phase63" "$OUTPUT_ROOT_ABS/collation/summary.json" \
  env \
    DBMIGRATE_COLLATION_MYSQL0900_SOURCE_SERVICE=mysql84a \
    DBMIGRATE_COLLATION_MYSQL0900_DEST_SERVICE=mariadb1011a \
    DBMIGRATE_COLLATION_UCA1400_SOURCE_SERVICE=mariadb114a \
    DBMIGRATE_COLLATION_UCA1400_DEST_SERVICE=mysql84b \
    DBMIGRATE_COLLATION_UCA1400_RISK_SOURCE_SERVICE=mariadb114a \
    DBMIGRATE_COLLATION_UCA1400_RISK_DEST_SERVICE=mariadb114b \
    DBMIGRATE_COLLATION_CLIENT_PROBE_SERVICE=mysql80a \
    "$SCRIPT_DIR/run-collation-rehearsal.sh" "$OUTPUT_ROOT_ABS/collation"

run_step "verify_canonicalization_phase64" "$OUTPUT_ROOT_ABS/verify-canonicalization/summary.json" \
  env \
    DBMIGRATE_VERIFY_CANONICAL_SOURCE_SERVICE=mysql84a \
    DBMIGRATE_VERIFY_CANONICAL_DEST_SERVICE=mariadb114b \
    "$SCRIPT_DIR/run-verify-canonicalization-rehearsal.sh" "$OUTPUT_ROOT_ABS/verify-canonicalization"

failed_steps="$(awk -F '\t' 'NR>1 && $2 != 0 {count++} END {print count+0}' "$MANIFEST_TSV")"
cat >"$SUMMARY_JSON" <<JSON
{
  "scenario": "v1_signoff_rehearsals",
  "output_root": "$(json_escape "$OUTPUT_ROOT_ABS")",
  "manifest": "$(json_escape "$MANIFEST_TSV")",
  "failed_steps": $failed_steps,
  "generated_at_utc": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
JSON

echo "Signoff rehearsal pack complete. Summary: $SUMMARY_JSON"
