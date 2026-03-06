#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <service> <output-dir>" >&2
  exit 1
fi

SERVICE="$1"
OUTPUT_DIR="$2"
SCHEMA_NAME="phase59_timezone"
TABLE_NAME="time_samples"
UTC_OFFSET="+00:00"
ALT_OFFSET="+02:00"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

compose() {
  docker compose -f "$PROJECT_ROOT/docker-compose.yml" "$@"
}

is_mariadb_service() {
  case "$1" in
    mariadb*) return 0 ;;
    *) return 1 ;;
  esac
}

client_bin() {
  if is_mariadb_service "$SERVICE"; then
    echo "mariadb"
  else
    echo "mysql"
  fi
}

db_exec() {
  compose exec -T "$SERVICE" "$(client_bin)" -u root -prootpass123 "$@"
}

db_exec_sql() {
  db_exec -e "$1"
}

wait_for_db() {
  local tries=60
  local i
  for i in $(seq 1 "$tries"); do
    if db_exec_sql "SELECT 1" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  echo "database service '$SERVICE' did not become healthy in time" >&2
  return 1
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

mkdir -p "$OUTPUT_DIR"

SERVER_VARIABLES_FILE="$OUTPUT_DIR/server-variables.txt"
UTC_QUERY_FILE="$OUTPUT_DIR/query-utc.tsv"
ALT_QUERY_FILE="$OUTPUT_DIR/query-alt.tsv"
SUMMARY_FILE="$OUTPUT_DIR/summary.json"

echo "Waiting for $SERVICE..."
wait_for_db

echo "Preparing schema..."
db_exec > /dev/null 2>&1 <<SQL
DROP DATABASE IF EXISTS \`$SCHEMA_NAME\`;
CREATE DATABASE \`$SCHEMA_NAME\`;
USE \`$SCHEMA_NAME\`;
CREATE TABLE \`$TABLE_NAME\` (
  sample_id INT AUTO_INCREMENT PRIMARY KEY,
  sample_label VARCHAR(32) NOT NULL,
  session_zone VARCHAR(16) NOT NULL,
  ts_auto TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  dt_auto DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  ts_explicit TIMESTAMP NULL,
  dt_explicit DATETIME NULL
);
SQL

db_exec -N -B -e "SELECT @@system_time_zone, @@global.time_zone, @@session.time_zone" >"$SERVER_VARIABLES_FILE"

echo "Inserting UTC sample..."
db_exec > /dev/null 2>&1 <<SQL
USE \`$SCHEMA_NAME\`;
SET SESSION time_zone = '$UTC_OFFSET';
INSERT INTO \`$TABLE_NAME\` (sample_label, session_zone, ts_explicit, dt_explicit)
VALUES ('utc_insert', '$UTC_OFFSET', NOW(), NOW());
SQL

echo "Inserting alternate-offset sample..."
db_exec > /dev/null 2>&1 <<SQL
USE \`$SCHEMA_NAME\`;
SET SESSION time_zone = '$ALT_OFFSET';
INSERT INTO \`$TABLE_NAME\` (sample_label, session_zone, ts_explicit, dt_explicit)
VALUES ('offset_insert', '$ALT_OFFSET', NOW(), NOW());
SQL

echo "Querying under UTC..."
db_exec -N -B <<SQL >"$UTC_QUERY_FILE"
USE \`$SCHEMA_NAME\`;
SET SESSION time_zone = '$UTC_OFFSET';
SELECT
  sample_label,
  session_zone,
  DATE_FORMAT(ts_auto, '%Y-%m-%d %H:%i:%s'),
  DATE_FORMAT(dt_auto, '%Y-%m-%d %H:%i:%s'),
  DATE_FORMAT(ts_explicit, '%Y-%m-%d %H:%i:%s'),
  DATE_FORMAT(dt_explicit, '%Y-%m-%d %H:%i:%s')
FROM \`$TABLE_NAME\`
ORDER BY sample_id;
SQL

echo "Querying under alternate offset..."
db_exec -N -B <<SQL >"$ALT_QUERY_FILE"
USE \`$SCHEMA_NAME\`;
SET SESSION time_zone = '$ALT_OFFSET';
SELECT
  sample_label,
  session_zone,
  DATE_FORMAT(ts_auto, '%Y-%m-%d %H:%i:%s'),
  DATE_FORMAT(dt_auto, '%Y-%m-%d %H:%i:%s'),
  DATE_FORMAT(ts_explicit, '%Y-%m-%d %H:%i:%s'),
  DATE_FORMAT(dt_explicit, '%Y-%m-%d %H:%i:%s')
FROM \`$TABLE_NAME\`
ORDER BY sample_id;
SQL

utc_first_ts_auto="$(awk -F '\t' 'NR==1 {print $3}' "$UTC_QUERY_FILE")"
alt_first_ts_auto="$(awk -F '\t' 'NR==1 {print $3}' "$ALT_QUERY_FILE")"
utc_first_dt_auto="$(awk -F '\t' 'NR==1 {print $4}' "$UTC_QUERY_FILE")"
alt_first_dt_auto="$(awk -F '\t' 'NR==1 {print $4}' "$ALT_QUERY_FILE")"

timestamp_display_changes="false"
if [ "$utc_first_ts_auto" != "$alt_first_ts_auto" ]; then
  timestamp_display_changes="true"
fi

datetime_static_under_session_change="false"
if [ "$utc_first_dt_auto" = "$alt_first_dt_auto" ]; then
  datetime_static_under_session_change="true"
fi

offset_row_utc_ts_explicit="$(awk -F '\t' 'NR==2 {print $5}' "$UTC_QUERY_FILE")"
offset_row_utc_dt_explicit="$(awk -F '\t' 'NR==2 {print $6}' "$UTC_QUERY_FILE")"
offset_row_alt_ts_explicit="$(awk -F '\t' 'NR==2 {print $5}' "$ALT_QUERY_FILE")"
offset_row_alt_dt_explicit="$(awk -F '\t' 'NR==2 {print $6}' "$ALT_QUERY_FILE")"

explicit_now_drift_visible="false"
if [ "$offset_row_utc_ts_explicit" != "$offset_row_alt_ts_explicit" ] && [ "$offset_row_utc_dt_explicit" = "$offset_row_alt_dt_explicit" ]; then
  explicit_now_drift_visible="true"
fi

cat >"$SUMMARY_FILE" <<JSON
{
  "scenario": "timezone_session_drift",
  "service": "$(json_escape "$SERVICE")",
  "engine_family": "$(json_escape "$(if is_mariadb_service "$SERVICE"; then echo mariadb; else echo mysql; fi)")",
  "system_time_zone": "$(json_escape "$(awk 'NR==1 {print $1}' "$SERVER_VARIABLES_FILE")")",
  "global_time_zone": "$(json_escape "$(awk 'NR==1 {print $2}' "$SERVER_VARIABLES_FILE")")",
  "session_time_zone_default": "$(json_escape "$(awk 'NR==1 {print $3}' "$SERVER_VARIABLES_FILE")")",
  "query_offsets": {
    "utc": "$UTC_OFFSET",
    "alternate": "$ALT_OFFSET"
  },
  "timestamp_display_changes": $timestamp_display_changes,
  "datetime_static_under_session_change": $datetime_static_under_session_change,
  "explicit_now_drift_visible": $explicit_now_drift_visible,
  "artifacts": {
    "server_variables": "$(json_escape "$SERVER_VARIABLES_FILE")",
    "query_utc": "$(json_escape "$UTC_QUERY_FILE")",
    "query_alt": "$(json_escape "$ALT_QUERY_FILE")"
  }
}
JSON

echo "Scenario complete. Summary: $SUMMARY_FILE"
