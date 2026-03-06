#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 3 ]; then
  echo "usage: $0 <source-service> <dest-service> <output-dir>" >&2
  exit 1
fi

SOURCE_SERVICE="$1"
DEST_SERVICE="$2"
OUTPUT_DIR="$3"
SOURCE_DB="phase62_hidden_schema"
DEST_DB_INCLUDED="phase62_hidden_schema_included"
DEST_DB_SKIPPED="phase62_hidden_schema_skipped"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
FIXTURE_TEMPLATE="$PROJECT_ROOT/datasets/phase62_mysql_hidden_schema.sql"

compose() {
  docker compose -f "$PROJECT_ROOT/docker-compose.yml" "$@"
}

is_mariadb_service() {
  case "$1" in
    mariadb*) return 0 ;;
    *) return 1 ;;
  esac
}

is_mysql_service() {
  case "$1" in
    mysql*) return 0 ;;
    *) return 1 ;;
  esac
}

client_bin() {
  if is_mariadb_service "$1"; then
    echo "mariadb"
  else
    echo "mysql"
  fi
}

dump_bin() {
  if is_mariadb_service "$1"; then
    echo "mariadb-dump"
  else
    echo "mysqldump"
  fi
}

service_port() {
  case "$1" in
    mariadb10) echo "13306" ;;
    mariadb11) echo "13307" ;;
    mariadb12) echo "13308" ;;
    mysql80) echo "23306" ;;
    mysql84) echo "23307" ;;
    *)
      echo "unknown service: $1" >&2
      return 1
      ;;
  esac
}

dsn_for_service() {
  local service="$1"
  local port
  port="$(service_port "$service")"
  printf 'mysql://root:rootpass123@127.0.0.1:%s/' "$port"
}

db_exec() {
  local service="$1"
  shift
  compose exec -T "$service" "$(client_bin "$service")" -u root -prootpass123 "$@"
}

db_exec_sql() {
  local service="$1"
  local sql="$2"
  db_exec "$service" -e "$sql"
}

wait_for_db() {
  local service="$1"
  local tries=60
  local i
  for i in $(seq 1 "$tries"); do
    if db_exec_sql "$service" "SELECT 1" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  echo "database service '$service' did not become healthy in time" >&2
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

contains_pattern() {
  local pattern="$1"
  local file="$2"
  if rg -q "$pattern" "$file"; then
    echo "true"
  else
    echo "false"
  fi
}

if ! is_mysql_service "$SOURCE_SERVICE"; then
  echo "source-service must be a MySQL service for Phase 62 hidden-schema rehearsal" >&2
  exit 1
fi

if [ ! -f "$FIXTURE_TEMPLATE" ]; then
  echo "missing fixture template: $FIXTURE_TEMPLATE" >&2
  exit 1
fi

mkdir -p "$OUTPUT_DIR"

SOURCE_FIXTURE_SQL="$OUTPUT_DIR/source-fixture.sql"
SOURCE_SHOW_CREATE="$OUTPUT_DIR/source-show-create.txt"
SOURCE_COLUMNS="$OUTPUT_DIR/source-columns.tsv"
SOURCE_INDEXES="$OUTPUT_DIR/source-indexes.tsv"
DUMP_INCLUDED="$OUTPUT_DIR/dump-included.sql"
DUMP_SKIPPED="$OUTPUT_DIR/dump-skipped.sql"
DEST_INCLUDED_SHOW_CREATE="$OUTPUT_DIR/dest-included-show-create.txt"
DEST_SKIPPED_SHOW_CREATE="$OUTPUT_DIR/dest-skipped-show-create.txt"
DEST_INCLUDED_COLUMNS="$OUTPUT_DIR/dest-included-columns.tsv"
DEST_SKIPPED_COLUMNS="$OUTPUT_DIR/dest-skipped-columns.tsv"
PLAN_OUTPUT="$OUTPUT_DIR/plan-output.json"
NOTES_FILE="$OUTPUT_DIR/notes.txt"
SUMMARY_FILE="$OUTPUT_DIR/summary.json"

echo "Waiting for $SOURCE_SERVICE..."
wait_for_db "$SOURCE_SERVICE"
echo "Waiting for $DEST_SERVICE..."
wait_for_db "$DEST_SERVICE"

sed "s/__DB__/$SOURCE_DB/g" "$FIXTURE_TEMPLATE" >"$SOURCE_FIXTURE_SQL"

echo "Loading source fixture into $SOURCE_SERVICE..."
db_exec "$SOURCE_SERVICE" <"$SOURCE_FIXTURE_SQL"

echo "Capturing source hidden-schema evidence..."
db_exec "$SOURCE_SERVICE" -N -B -e "SHOW CREATE TABLE \`$SOURCE_DB\`.\`invisible_demo\`; SHOW CREATE TABLE \`$SOURCE_DB\`.\`gipk_demo\`;" >"$SOURCE_SHOW_CREATE"
db_exec "$SOURCE_SERVICE" -N -B -e "SELECT TABLE_NAME, COLUMN_NAME, EXTRA, COLUMN_KEY FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = '$SOURCE_DB' ORDER BY TABLE_NAME, ORDINAL_POSITION;" >"$SOURCE_COLUMNS"
db_exec "$SOURCE_SERVICE" -N -B -e "SELECT TABLE_NAME, INDEX_NAME, IS_VISIBLE FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = '$SOURCE_DB' ORDER BY TABLE_NAME, INDEX_NAME;" >"$SOURCE_INDEXES"

echo "Creating logical dumps with and without GIPK..."
compose exec -T "$SOURCE_SERVICE" "$(dump_bin "$SOURCE_SERVICE")" -u root -prootpass123 --skip-dump-date "$SOURCE_DB" >"$DUMP_INCLUDED"
compose exec -T "$SOURCE_SERVICE" "$(dump_bin "$SOURCE_SERVICE")" -u root -prootpass123 --skip-dump-date --skip-generated-invisible-primary-key "$SOURCE_DB" >"$DUMP_SKIPPED"

echo "Preparing destination databases..."
db_exec_sql "$DEST_SERVICE" "DROP DATABASE IF EXISTS \`$DEST_DB_INCLUDED\`; DROP DATABASE IF EXISTS \`$DEST_DB_SKIPPED\`; CREATE DATABASE \`$DEST_DB_INCLUDED\`; CREATE DATABASE \`$DEST_DB_SKIPPED\`;"

echo "Restoring included dump into $DEST_SERVICE..."
db_exec "$DEST_SERVICE" "$DEST_DB_INCLUDED" <"$DUMP_INCLUDED"

echo "Restoring skipped dump into $DEST_SERVICE..."
db_exec "$DEST_SERVICE" "$DEST_DB_SKIPPED" <"$DUMP_SKIPPED"

echo "Capturing destination evidence..."
db_exec "$DEST_SERVICE" -N -B -e "SHOW CREATE TABLE \`$DEST_DB_INCLUDED\`.\`invisible_demo\`; SHOW CREATE TABLE \`$DEST_DB_INCLUDED\`.\`gipk_demo\`;" >"$DEST_INCLUDED_SHOW_CREATE"
db_exec "$DEST_SERVICE" -N -B -e "SHOW CREATE TABLE \`$DEST_DB_SKIPPED\`.\`invisible_demo\`; SHOW CREATE TABLE \`$DEST_DB_SKIPPED\`.\`gipk_demo\`;" >"$DEST_SKIPPED_SHOW_CREATE"
db_exec "$DEST_SERVICE" -N -B -e "SELECT TABLE_NAME, COLUMN_NAME, EXTRA, COLUMN_KEY FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = '$DEST_DB_INCLUDED' ORDER BY TABLE_NAME, ORDINAL_POSITION;" >"$DEST_INCLUDED_COLUMNS"
db_exec "$DEST_SERVICE" -N -B -e "SELECT TABLE_NAME, COLUMN_NAME, EXTRA, COLUMN_KEY FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = '$DEST_DB_SKIPPED' ORDER BY TABLE_NAME, ORDINAL_POSITION;" >"$DEST_SKIPPED_COLUMNS"

echo "Running dbmigrate plan for the source/destination pair..."
set +e
"$PROJECT_ROOT/bin/dbmigrate" plan \
  --source "$(dsn_for_service "$SOURCE_SERVICE")" \
  --dest "$(dsn_for_service "$DEST_SERVICE")" \
  --databases "$SOURCE_DB" \
  --downgrade-profile max-compat \
  --json >"$PLAN_OUTPUT"
plan_exit_code=$?
set -e

source_invisible_column_count="$(rg -c $'invisible_demo\tsecret_token\t.*INVISIBLE' "$SOURCE_COLUMNS" || true)"
source_invisible_index_count="$(rg -c $'invisible_demo\tidx_secret_token\tNO' "$SOURCE_INDEXES" || true)"
source_gipk_count="$(rg -c $'gipk_demo\tmy_row_id\t.*INVISIBLE.*\tPRI' "$SOURCE_COLUMNS" || true)"

included_invisible_column_preserved="$(contains_pattern 'secret_token.*INVISIBLE' "$DEST_INCLUDED_SHOW_CREATE")"
included_invisible_index_preserved="$(contains_pattern 'idx_secret_token.*INVISIBLE' "$DEST_INCLUDED_SHOW_CREATE")"
included_gipk_column_present="$(contains_pattern 'my_row_id' "$DEST_INCLUDED_SHOW_CREATE")"
included_gipk_remains_invisible="$(contains_pattern 'my_row_id.*INVISIBLE' "$DEST_INCLUDED_SHOW_CREATE")"
skipped_gipk_column_present="$(contains_pattern 'my_row_id' "$DEST_SKIPPED_SHOW_CREATE")"

visibility_drift_detected="false"
if [ "$included_invisible_column_preserved" != "true" ] || [ "$included_invisible_index_preserved" != "true" ] || [ "$included_gipk_remains_invisible" != "true" ]; then
  visibility_drift_detected="true"
fi

cat >"$NOTES_FILE" <<EOF
Phase 62 rehearsal compares the same MySQL source fixture under two logical dump modes:
- included dump: default mysqldump behavior with generated invisible primary keys preserved
- skipped dump: mysqldump --skip-generated-invisible-primary-key

Interpretation:
- included_invisible_column_preserved=$included_invisible_column_preserved
- included_invisible_index_preserved=$included_invisible_index_preserved
- included_gipk_column_present=$included_gipk_column_present
- included_gipk_remains_invisible=$included_gipk_remains_invisible
- skipped_gipk_column_present=$skipped_gipk_column_present
- visibility_drift_detected=$visibility_drift_detected

If the destination is MariaDB, visibility drift is expected: MySQL versioned INVISIBLE comments are accepted syntactically but materialized as normal visible schema objects.
If the destination is MySQL 8.0.30+, default dumps preserve GIPK while --skip-generated-invisible-primary-key removes it from the logical schema entirely.
EOF

cat >"$SUMMARY_FILE" <<JSON
{
  "scenario": "invisible_gipk_downgrade_evidence",
  "source_service": "$(json_escape "$SOURCE_SERVICE")",
  "dest_service": "$(json_escape "$DEST_SERVICE")",
  "source_dsn": "$(json_escape "$(dsn_for_service "$SOURCE_SERVICE")")",
  "dest_dsn": "$(json_escape "$(dsn_for_service "$DEST_SERVICE")")",
  "plan_exit_code": $plan_exit_code,
  "source_invisible_column_count": ${source_invisible_column_count:-0},
  "source_invisible_index_count": ${source_invisible_index_count:-0},
  "source_gipk_table_count": ${source_gipk_count:-0},
  "included_invisible_column_preserved": $included_invisible_column_preserved,
  "included_invisible_index_preserved": $included_invisible_index_preserved,
  "included_gipk_column_present": $included_gipk_column_present,
  "included_gipk_remains_invisible": $included_gipk_remains_invisible,
  "skipped_gipk_column_present": $skipped_gipk_column_present,
  "visibility_drift_detected": $visibility_drift_detected,
  "artifacts": {
    "source_fixture": "$(json_escape "$SOURCE_FIXTURE_SQL")",
    "source_show_create": "$(json_escape "$SOURCE_SHOW_CREATE")",
    "source_columns": "$(json_escape "$SOURCE_COLUMNS")",
    "source_indexes": "$(json_escape "$SOURCE_INDEXES")",
    "dump_included": "$(json_escape "$DUMP_INCLUDED")",
    "dump_skipped": "$(json_escape "$DUMP_SKIPPED")",
    "dest_included_show_create": "$(json_escape "$DEST_INCLUDED_SHOW_CREATE")",
    "dest_skipped_show_create": "$(json_escape "$DEST_SKIPPED_SHOW_CREATE")",
    "dest_included_columns": "$(json_escape "$DEST_INCLUDED_COLUMNS")",
    "dest_skipped_columns": "$(json_escape "$DEST_SKIPPED_COLUMNS")",
    "plan_output": "$(json_escape "$PLAN_OUTPUT")",
    "notes": "$(json_escape "$NOTES_FILE")"
  }
}
JSON

echo "Scenario complete. Summary: $SUMMARY_FILE"
