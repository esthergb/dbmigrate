#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <service> <output-dir>" >&2
  exit 1
fi

SERVICE="$1"
OUTPUT_DIR="$2"
SOURCE_DB="phase58_backup_source"
RESTORE_DB="phase58_backup_restore"
TABLE_NAME="items"
VIEW_NAME="v_items"
PROCEDURE_NAME="count_items"
EVENT_NAME="refresh_marker"
MARKER_TABLE="restore_markers"

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

dump_bin() {
  if is_mariadb_service "$SERVICE"; then
    echo "mariadb-dump"
  else
    echo "mysqldump"
  fi
}

server_version() {
  db_exec -N -B -e "SELECT VERSION();"
}

dump_client_version() {
  compose exec -T "$SERVICE" "$(dump_bin)" --version | head -n 1
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

DUMP_FILE="$OUTPUT_DIR/logical-backup.sql"
VALIDATION_FILE="$OUTPUT_DIR/validation.txt"
RESTORE_SMOKE_FILE="$OUTPUT_DIR/restore-smoke.txt"
SUMMARY_FILE="$OUTPUT_DIR/summary.json"

DUMP_CLIENT_VERSION="$(dump_client_version)"
SERVER_VERSION="$(server_version)"

echo "Waiting for $SERVICE..."
wait_for_db

echo "Preparing source schema..."
db_exec > /dev/null 2>&1 <<SQL
DROP DATABASE IF EXISTS \`$SOURCE_DB\`;
DROP DATABASE IF EXISTS \`$RESTORE_DB\`;
CREATE DATABASE \`$SOURCE_DB\`;
USE \`$SOURCE_DB\`;
CREATE TABLE \`$TABLE_NAME\` (
  id INT PRIMARY KEY,
  note VARCHAR(64) NOT NULL
);
INSERT INTO \`$TABLE_NAME\` (id, note) VALUES (1, 'alpha'), (2, 'beta');
CREATE VIEW \`$VIEW_NAME\` AS
  SELECT id, note FROM \`$TABLE_NAME\` WHERE id >= 1;
CREATE TABLE \`$MARKER_TABLE\` (
  marker_id INT PRIMARY KEY,
  marker_note VARCHAR(64) NOT NULL
);
INSERT INTO \`$MARKER_TABLE\` (marker_id, marker_note) VALUES (1, 'ready');
DELIMITER //
CREATE PROCEDURE \`$PROCEDURE_NAME\`(OUT total_rows INT)
BEGIN
  SELECT COUNT(*) INTO total_rows FROM \`$TABLE_NAME\`;
END//
DELIMITER ;
CREATE EVENT \`$EVENT_NAME\`
  ON SCHEDULE EVERY 1 DAY
  DO
    INSERT INTO \`$MARKER_TABLE\` (marker_id, marker_note) VALUES (2, 'event-fired');
SQL

echo "Creating logical backup..."
if compose exec -T "$SERVICE" "$(dump_bin)" -u root -prootpass123 --routines --triggers --events --single-transaction --skip-dump-date "$SOURCE_DB" >"$DUMP_FILE"; then
  backup_completed="true"
else
  backup_completed="false"
fi

backup_validated="false"
if [ "$backup_completed" = "true" ] && [ -s "$DUMP_FILE" ]; then
  {
    echo "dump_nonempty=true"
    echo "dump_client_version=$DUMP_CLIENT_VERSION"
    echo "server_version=$SERVER_VERSION"
    if rg -q "CREATE TABLE \`$TABLE_NAME\`" "$DUMP_FILE"; then
      echo "table_definition_present=true"
    else
      echo "table_definition_present=false"
    fi
    if rg -q "VIEW \`$VIEW_NAME\`" "$DUMP_FILE"; then
      echo "view_definition_present=true"
    else
      echo "view_definition_present=false"
    fi
    if rg -q "PROCEDURE \`$PROCEDURE_NAME\`" "$DUMP_FILE"; then
      echo "procedure_definition_present=true"
    else
      echo "procedure_definition_present=false"
    fi
    if rg -q "EVENT \`$EVENT_NAME\`" "$DUMP_FILE"; then
      echo "event_definition_present=true"
    else
      echo "event_definition_present=false"
    fi
  } >"$VALIDATION_FILE"

  if rg -q "table_definition_present=true" "$VALIDATION_FILE" &&
    rg -q "view_definition_present=true" "$VALIDATION_FILE" &&
    rg -q "procedure_definition_present=true" "$VALIDATION_FILE" &&
    rg -q "event_definition_present=true" "$VALIDATION_FILE"; then
    backup_validated="true"
  fi
else
  {
    echo "dump_nonempty=false"
    echo "dump_client_version=$DUMP_CLIENT_VERSION"
    echo "server_version=$SERVER_VERSION"
    echo "table_definition_present=false"
    echo "view_definition_present=false"
    echo "procedure_definition_present=false"
    echo "event_definition_present=false"
  } >"$VALIDATION_FILE"
fi

restore_usable="false"
restore_exit_code=1
if [ "$backup_completed" = "true" ]; then
  echo "Restoring backup into shadow schema..."
  db_exec_sql "CREATE DATABASE \`$RESTORE_DB\`;"
  set +e
  db_exec "$RESTORE_DB" <"$DUMP_FILE"
  restore_exit_code=$?
  set -e
  if [ "$restore_exit_code" -eq 0 ]; then
    row_count="$(db_exec -N -B "$RESTORE_DB" -e "SELECT COUNT(*) FROM \`$TABLE_NAME\`;")"
    view_count="$(db_exec -N -B "$RESTORE_DB" -e "SELECT COUNT(*) FROM \`$VIEW_NAME\`;")"
    proc_count="$(db_exec -N -B "$RESTORE_DB" -e "CALL \`$PROCEDURE_NAME\`(@total_rows); SELECT @total_rows;")"
    event_count="$(db_exec -N -B "$RESTORE_DB" -e "SELECT COUNT(*) FROM information_schema.EVENTS WHERE EVENT_SCHEMA = '$RESTORE_DB' AND EVENT_NAME = '$EVENT_NAME';")"
    {
      echo "row_count=$row_count"
      echo "view_count=$view_count"
      echo "procedure_count=$proc_count"
      echo "event_count=$event_count"
    } >"$RESTORE_SMOKE_FILE"
    if [ "$row_count" = "2" ] && [ "$view_count" = "2" ] && [ "$proc_count" = "2" ] && [ "$event_count" = "1" ]; then
      restore_usable="true"
    fi
  else
    {
      echo "restore_exit_code=$restore_exit_code"
      echo "restore_failed=true"
    } >"$RESTORE_SMOKE_FILE"
  fi
fi

cat >"$SUMMARY_FILE" <<JSON
{
  "scenario": "backup_restore_rehearsal_required",
  "service": "$(json_escape "$SERVICE")",
  "engine_family": "$(json_escape "$(if is_mariadb_service "$SERVICE"; then echo mariadb; else echo mysql; fi)")",
  "dump_client": "$(json_escape "$(dump_bin)")",
  "dump_client_version": "$(json_escape "$DUMP_CLIENT_VERSION")",
  "server_version": "$(json_escape "$SERVER_VERSION")",
  "source_db": "$(json_escape "$SOURCE_DB")",
  "restore_db": "$(json_escape "$RESTORE_DB")",
  "backup_completed": $backup_completed,
  "backup_validated": $backup_validated,
  "restore_usable": $restore_usable,
  "restore_exit_code": $restore_exit_code,
  "artifacts": {
    "dump_file": "$(json_escape "$DUMP_FILE")",
    "validation": "$(json_escape "$VALIDATION_FILE")",
    "restore_smoke": "$(json_escape "$RESTORE_SMOKE_FILE")"
  }
}
JSON

echo "Scenario complete. Summary: $SUMMARY_FILE"
