#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <service> <output-dir>" >&2
  exit 1
fi

SERVICE="$1"
OUTPUT_DIR="$2"
SCHEMA_NAME="phase57_metadata_lock"
TABLE_NAME="items"
BLOCKER_SLEEP_SECONDS="${DBMIGRATE_METADATA_LOCK_BLOCKER_SLEEP:-12}"
DDL_LOCK_WAIT_SECONDS="${DBMIGRATE_METADATA_LOCK_WAIT_TIMEOUT:-5}"
READ_LOCK_WAIT_SECONDS="${DBMIGRATE_METADATA_LOCK_READ_TIMEOUT:-10}"

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

cleanup_background() {
  local pid
  for pid in "${BLOCKER_PID:-}" "${DDL_PID:-}" "${READ_PID:-}"; do
    if [ -n "${pid:-}" ] && kill -0 "$pid" >/dev/null 2>&1; then
      kill "$pid" >/dev/null 2>&1 || true
      wait "$pid" >/dev/null 2>&1 || true
    fi
  done
}

trap cleanup_background EXIT

mkdir -p "$OUTPUT_DIR"

BLOCKER_LOG="$OUTPUT_DIR/blocker.log"
DDL_LOG="$OUTPUT_DIR/ddl.log"
READ_LOG="$OUTPUT_DIR/read.log"
DDL_META="$OUTPUT_DIR/ddl.meta"
READ_META="$OUTPUT_DIR/read.meta"
PROCESSLIST_FILE="$OUTPUT_DIR/processlist.txt"
METADATA_LOCKS_FILE="$OUTPUT_DIR/metadata-locks.tsv"
SERVER_VARIABLES_FILE="$OUTPUT_DIR/server-variables.txt"
SUMMARY_FILE="$OUTPUT_DIR/summary.json"
PLUGIN_FILE="$OUTPUT_DIR/plugin-attempt.txt"

: >"$PLUGIN_FILE"

echo "Waiting for $SERVICE..."
wait_for_db

echo "Preparing schema..."
db_exec_sql "DROP DATABASE IF EXISTS \`$SCHEMA_NAME\`; CREATE DATABASE \`$SCHEMA_NAME\`; CREATE TABLE \`$SCHEMA_NAME\`.\`$TABLE_NAME\` (id INT PRIMARY KEY, note VARCHAR(64) NOT NULL); INSERT INTO \`$SCHEMA_NAME\`.\`$TABLE_NAME\` (id, note) VALUES (1, 'seed'), (2, 'seed');"
db_exec_sql "SHOW VARIABLES LIKE 'performance_schema'; SHOW VARIABLES LIKE 'lock_wait_timeout';" >"$SERVER_VARIABLES_FILE"

if is_mariadb_service "$SERVICE"; then
  {
    echo "MariaDB metadata_lock_info plugin is not auto-installed by this script."
    echo "If metadata lock instrumentation is unavailable, use processlist plus optional manual plugin enablement."
  } >"$PLUGIN_FILE"
fi

echo "Starting blocker transaction..."
(
  db_exec >"$BLOCKER_LOG" 2>&1 <<SQL
USE \`$SCHEMA_NAME\`;
SET SESSION lock_wait_timeout = 60;
START TRANSACTION;
SELECT id FROM \`$TABLE_NAME\` WHERE id = 1;
SELECT SLEEP($BLOCKER_SLEEP_SECONDS);
COMMIT;
SQL
) &
BLOCKER_PID=$!

sleep 2

echo "Starting blocked DDL..."
(
  start_epoch="$(date +%s)"
  set +e
  db_exec >"$DDL_LOG" 2>&1 <<SQL
USE \`$SCHEMA_NAME\`;
SET SESSION lock_wait_timeout = $DDL_LOCK_WAIT_SECONDS;
ALTER TABLE \`$TABLE_NAME\` ADD COLUMN phase57_probe INT NULL;
SQL
  ddl_rc=$?
  set -e
  end_epoch="$(date +%s)"
  printf 'rc=%s\nelapsed=%s\n' "$ddl_rc" "$((end_epoch-start_epoch))" >"$DDL_META"
) &
DDL_PID=$!

sleep 1

echo "Starting queued reader..."
(
  start_epoch="$(date +%s)"
  set +e
  db_exec >"$READ_LOG" 2>&1 <<SQL
USE \`$SCHEMA_NAME\`;
SET SESSION lock_wait_timeout = $READ_LOCK_WAIT_SECONDS;
SELECT COUNT(*) AS item_count FROM \`$TABLE_NAME\`;
SQL
  read_rc=$?
  set -e
  end_epoch="$(date +%s)"
  printf 'rc=%s\nelapsed=%s\n' "$read_rc" "$((end_epoch-start_epoch))" >"$READ_META"
) &
READ_PID=$!

sleep 2

echo "Capturing observability artifacts..."
db_exec_sql "SHOW FULL PROCESSLIST" >"$PROCESSLIST_FILE" || true

metadata_locks_available="false"
if db_exec_sql "SELECT 1 FROM performance_schema.metadata_locks LIMIT 1" >/dev/null 2>&1; then
  metadata_locks_available="true"
  db_exec_sql "SELECT * FROM performance_schema.metadata_locks WHERE OBJECT_SCHEMA = '$SCHEMA_NAME'" >"$METADATA_LOCKS_FILE" || true
else
  echo "performance_schema.metadata_locks not available on $SERVICE" >"$METADATA_LOCKS_FILE"
fi

wait "$DDL_PID" || true
wait "$READ_PID" || true
wait "$BLOCKER_PID" || true

ddl_rc="$(awk -F= '/^rc=/{print $2}' "$DDL_META" 2>/dev/null || echo 1)"
ddl_elapsed="$(awk -F= '/^elapsed=/{print $2}' "$DDL_META" 2>/dev/null || echo 0)"
read_rc="$(awk -F= '/^rc=/{print $2}' "$READ_META" 2>/dev/null || echo 1)"
read_elapsed="$(awk -F= '/^elapsed=/{print $2}' "$READ_META" 2>/dev/null || echo 0)"

queue_amplification="false"
if [ "$read_rc" -eq 0 ] && [ "$read_elapsed" -ge 2 ]; then
  queue_amplification="true"
fi

cat >"$SUMMARY_FILE" <<JSON
{
  "scenario": "metadata_lock_queue_amplification",
  "service": "$(json_escape "$SERVICE")",
  "engine_family": "$(json_escape "$(if is_mariadb_service "$SERVICE"; then echo mariadb; else echo mysql; fi)")",
  "schema": "$(json_escape "$SCHEMA_NAME")",
  "table": "$(json_escape "$TABLE_NAME")",
  "blocker_sleep_seconds": $BLOCKER_SLEEP_SECONDS,
  "ddl_lock_wait_seconds": $DDL_LOCK_WAIT_SECONDS,
  "read_lock_wait_seconds": $READ_LOCK_WAIT_SECONDS,
  "ddl_exit_code": $ddl_rc,
  "ddl_elapsed_seconds": $ddl_elapsed,
  "read_exit_code": $read_rc,
  "read_elapsed_seconds": $read_elapsed,
  "queue_amplification_detected": $queue_amplification,
  "metadata_locks_available": $metadata_locks_available,
  "artifacts": {
    "server_variables": "$(json_escape "$SERVER_VARIABLES_FILE")",
    "processlist": "$(json_escape "$PROCESSLIST_FILE")",
    "metadata_locks": "$(json_escape "$METADATA_LOCKS_FILE")",
    "plugin_attempt": "$(json_escape "$PLUGIN_FILE")",
    "blocker_log": "$(json_escape "$BLOCKER_LOG")",
    "ddl_log": "$(json_escape "$DDL_LOG")",
    "read_log": "$(json_escape "$READ_LOG")"
  }
}
JSON

echo "Scenario complete. Summary: $SUMMARY_FILE"
