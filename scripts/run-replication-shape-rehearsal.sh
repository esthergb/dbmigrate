#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <service> <output-dir>" >&2
  exit 1
fi

SERVICE="$1"
OUTPUT_DIR="$2"
SCHEMA_NAME="phase61_parallelism"
PARENT_TABLE="parent_items"
CHILD_TABLE="child_items"
TOTAL_ROWS=1000
CHUNK_SIZE=50
CHUNK_COUNT=$((TOTAL_ROWS / CHUNK_SIZE))

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

run_timed_sql_file() {
  local sql_file="$1"
  local log_file="$2"
  local meta_file="$3"
  local start_epoch end_epoch rc
  start_epoch="$(date +%s)"
  set +e
  db_exec <"$sql_file" >"$log_file" 2>&1
  rc=$?
  set -e
  end_epoch="$(date +%s)"
  printf 'rc=%s\nelapsed=%s\n' "$rc" "$((end_epoch-start_epoch))" >"$meta_file"
  return 0
}

mkdir -p "$OUTPUT_DIR"

SETUP_SQL="$OUTPUT_DIR/setup.sql"
MONOLITHIC_SQL="$OUTPUT_DIR/monolithic.sql"
CHUNKED_SQL="$OUTPUT_DIR/chunked.sql"
MONOLITHIC_LOG="$OUTPUT_DIR/monolithic.log"
CHUNKED_LOG="$OUTPUT_DIR/chunked.log"
MONOLITHIC_META="$OUTPUT_DIR/monolithic.meta"
CHUNKED_META="$OUTPUT_DIR/chunked.meta"
NOTES_FILE="$OUTPUT_DIR/notes.txt"
SUMMARY_FILE="$OUTPUT_DIR/summary.json"

echo "Waiting for $SERVICE..."
wait_for_db

cat >"$SETUP_SQL" <<SQL
DROP DATABASE IF EXISTS \`$SCHEMA_NAME\`;
CREATE DATABASE \`$SCHEMA_NAME\`;
USE \`$SCHEMA_NAME\`;
CREATE TABLE \`$PARENT_TABLE\` (
  parent_id INT PRIMARY KEY,
  note VARCHAR(32) NOT NULL
) ENGINE=InnoDB;
CREATE TABLE \`$CHILD_TABLE\` (
  row_id INT PRIMARY KEY,
  parent_id INT NOT NULL,
  payload VARCHAR(64) NOT NULL,
  CONSTRAINT fk_parent FOREIGN KEY (parent_id) REFERENCES \`$PARENT_TABLE\`(parent_id)
) ENGINE=InnoDB;
INSERT INTO \`$PARENT_TABLE\` (parent_id, note) VALUES (1, 'ready');
SQL
db_exec <"$SETUP_SQL" >/dev/null

{
  echo "USE \`$SCHEMA_NAME\`;"
  echo "TRUNCATE TABLE \`$CHILD_TABLE\`;"
  echo "START TRANSACTION;"
  for i in $(seq 1 "$TOTAL_ROWS"); do
    printf "INSERT INTO \`%s\` (row_id, parent_id, payload) VALUES (%d, 1, 'mono-%04d');\n" "$CHILD_TABLE" "$i" "$i"
  done
  echo "COMMIT;"
} >"$MONOLITHIC_SQL"

{
  echo "USE \`$SCHEMA_NAME\`;"
  echo "TRUNCATE TABLE \`$CHILD_TABLE\`;"
  row_id=1
  for chunk in $(seq 1 "$CHUNK_COUNT"); do
    echo "START TRANSACTION;"
    for _ in $(seq 1 "$CHUNK_SIZE"); do
      printf "INSERT INTO \`%s\` (row_id, parent_id, payload) VALUES (%d, 1, 'chunk-%04d');\n" "$CHILD_TABLE" "$row_id" "$row_id"
      row_id=$((row_id + 1))
    done
    echo "COMMIT;"
  done
} >"$CHUNKED_SQL"

echo "Running monolithic transaction rehearsal..."
run_timed_sql_file "$MONOLITHIC_SQL" "$MONOLITHIC_LOG" "$MONOLITHIC_META"
monolithic_rc="$(awk -F= '/^rc=/{print $2}' "$MONOLITHIC_META")"
monolithic_elapsed="$(awk -F= '/^elapsed=/{print $2}' "$MONOLITHIC_META")"
monolithic_rows="$(db_exec -N -B -e "SELECT COUNT(*) FROM \`$SCHEMA_NAME\`.\`$CHILD_TABLE\`;")"

echo "Running chunked transaction rehearsal..."
run_timed_sql_file "$CHUNKED_SQL" "$CHUNKED_LOG" "$CHUNKED_META"
chunked_rc="$(awk -F= '/^rc=/{print $2}' "$CHUNKED_META")"
chunked_elapsed="$(awk -F= '/^elapsed=/{print $2}' "$CHUNKED_META")"
chunked_rows="$(db_exec -N -B -e "SELECT COUNT(*) FROM \`$SCHEMA_NAME\`.\`$CHILD_TABLE\`;")"

{
  echo "This rehearsal is about transaction shape, not raw single-node throughput."
  echo "A parallel replica or apply pipeline still commits at transaction boundaries."
  echo "One huge FK-bound transaction creates a single serialization unit."
  echo "Chunked commits create smaller recovery and scheduling units, even when total rows are identical."
  echo "Compare row counts, transaction counts, and max rows per transaction rather than only elapsed wall-clock time."
} >"$NOTES_FILE"

cat >"$SUMMARY_FILE" <<JSON
{
  "scenario": "replication_parallelism_vs_chunking",
  "service": "$(json_escape "$SERVICE")",
  "fk_enabled": true,
  "total_rows": $TOTAL_ROWS,
  "monolithic": {
    "transaction_count": 1,
    "max_rows_per_transaction": $TOTAL_ROWS,
    "exit_code": $monolithic_rc,
    "elapsed_seconds": $monolithic_elapsed,
    "rows_written": $monolithic_rows
  },
  "chunked": {
    "transaction_count": $CHUNK_COUNT,
    "chunk_size": $CHUNK_SIZE,
    "max_rows_per_transaction": $CHUNK_SIZE,
    "exit_code": $chunked_rc,
    "elapsed_seconds": $chunked_elapsed,
    "rows_written": $chunked_rows
  },
  "shape_signal": {
    "same_total_rows": true,
    "monolithic_dominates_transaction_shape": true,
    "chunked_reduces_commit_granularity": true
  },
  "artifacts": {
    "setup_sql": "$(json_escape "$SETUP_SQL")",
    "monolithic_sql": "$(json_escape "$MONOLITHIC_SQL")",
    "chunked_sql": "$(json_escape "$CHUNKED_SQL")",
    "monolithic_log": "$(json_escape "$MONOLITHIC_LOG")",
    "chunked_log": "$(json_escape "$CHUNKED_LOG")",
    "notes": "$(json_escape "$NOTES_FILE")"
  }
}
JSON

echo "Scenario complete. Summary: $SUMMARY_FILE"
