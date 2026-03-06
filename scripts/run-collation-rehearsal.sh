#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: $0 <output-dir>" >&2
  exit 1
fi

OUTPUT_DIR="$1"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
MYSQL0900_FIXTURE="$PROJECT_ROOT/datasets/phase63_mysql0900_collation.sql"
MARIADB_UCA1400_FIXTURE="$PROJECT_ROOT/datasets/phase63_mariadb_uca1400_collation.sql"

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
  printf 'mysql://root:rootpass123@127.0.0.1:%s/' "$(service_port "$service")"
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

count_plan_code() {
  local code="$1"
  local file="$2"
  rg -c "\"code\": \"$code\"" "$file" || true
}

run_client_probe() {
  local client_service="$1"
  local target_service="$2"
  local sql="$3"
  local out_file="$4"
  local err_file="$5"
  set +e
  compose exec -T "$client_service" "$(client_bin "$client_service")" \
    -h host.docker.internal \
    -P "$(service_port "$target_service")" \
    -u root -prootpass123 -N -B -e "$sql" >"$out_file" 2>"$err_file"
  local exit_code=$?
  set -e
  echo "$exit_code"
}

run_plan_and_report() {
  local source_service="$1"
  local dest_service="$2"
  local database_name="$3"
  local state_dir="$4"
  local plan_output="$5"
  local report_output="$6"

  mkdir -p "$state_dir"

  set +e
  "$PROJECT_ROOT/bin/dbmigrate" plan \
    --source "$(dsn_for_service "$source_service")" \
    --dest "$(dsn_for_service "$dest_service")" \
    --databases "$database_name" \
    --downgrade-profile max-compat \
    --state-dir "$state_dir" \
    --json >"$plan_output"
  local plan_exit_code=$?
  set -e

  "$PROJECT_ROOT/bin/dbmigrate" report \
    --state-dir "$state_dir" \
    --json >"$report_output"

  echo "$plan_exit_code"
}

run_restore_attempt() {
  local source_service="$1"
  local dest_service="$2"
  local database_name="$3"
  local dump_output="$4"
  local import_log="$5"

  compose exec -T "$source_service" "$(dump_bin "$source_service")" -u root -prootpass123 --skip-dump-date "$database_name" >"$dump_output"
  db_exec_sql "$dest_service" "DROP DATABASE IF EXISTS \`$database_name\`; CREATE DATABASE \`$database_name\`;"

  set +e
  db_exec "$dest_service" "$database_name" <"$dump_output" > /dev/null 2>"$import_log"
  local import_exit_code=$?
  set -e
  echo "$import_exit_code"
}

prepare_fixture() {
  local fixture_template="$1"
  local database_name="$2"
  local output_file="$3"
  sed "s/__DB__/$database_name/g" "$fixture_template" >"$output_file"
}

capture_collation_inventory() {
  local service="$1"
  local database_name="$2"
  local out_file="$3"
  db_exec "$service" -N -B -e "
    SELECT 'schema', SCHEMA_NAME, '', '', DEFAULT_COLLATION_NAME
    FROM information_schema.SCHEMATA
    WHERE SCHEMA_NAME = '$database_name'
    UNION ALL
    SELECT 'table', TABLE_SCHEMA, TABLE_NAME, '', TABLE_COLLATION
    FROM information_schema.TABLES
    WHERE TABLE_SCHEMA = '$database_name' AND COALESCE(TABLE_COLLATION, '') <> ''
    UNION ALL
    SELECT 'column', TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME, COLLATION_NAME
    FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = '$database_name' AND COALESCE(COLLATION_NAME, '') <> ''
    ORDER BY 1, 2, 3, 4;
  " >"$out_file"
}

mkdir -p "$OUTPUT_DIR"

for service in mysql80 mysql84 mariadb10 mariadb12; do
  echo "Waiting for $service..."
  wait_for_db "$service"
done

MYSQL0900_DIR="$OUTPUT_DIR/mysql84-to-mariadb10-0900"
MARIADB_UCA1400_TO_MYSQL_DIR="$OUTPUT_DIR/mariadb12-to-mysql84-uca1400"
MARIADB_UCA1400_RISK_DIR="$OUTPUT_DIR/mariadb12-to-mariadb12-uca1400"
mkdir -p "$MYSQL0900_DIR" "$MARIADB_UCA1400_TO_MYSQL_DIR" "$MARIADB_UCA1400_RISK_DIR"

MYSQL0900_DB="phase63_mysql0900"
MYSQL0900_SOURCE_SQL="$MYSQL0900_DIR/source-fixture.sql"
MYSQL0900_SOURCE_COLLATIONS="$MYSQL0900_DIR/source-collations.tsv"
MYSQL0900_PLAN="$MYSQL0900_DIR/plan-output.json"
MYSQL0900_REPORT="$MYSQL0900_DIR/report-output.json"
MYSQL0900_DUMP="$MYSQL0900_DIR/dump.sql"
MYSQL0900_IMPORT_LOG="$MYSQL0900_DIR/import.stderr.log"
MYSQL0900_CLIENT_PROBE="$MYSQL0900_DIR/client-probe.txt"
MYSQL0900_CLIENT_PROBE_ERR="$MYSQL0900_DIR/client-probe.stderr.log"
MYSQL0900_SUMMARY="$MYSQL0900_DIR/summary.json"

prepare_fixture "$MYSQL0900_FIXTURE" "$MYSQL0900_DB" "$MYSQL0900_SOURCE_SQL"
db_exec mysql84 <"$MYSQL0900_SOURCE_SQL"
capture_collation_inventory mysql84 "$MYSQL0900_DB" "$MYSQL0900_SOURCE_COLLATIONS"
mysql0900_plan_exit="$(run_plan_and_report mysql84 mariadb10 "$MYSQL0900_DB" "$MYSQL0900_DIR/state" "$MYSQL0900_PLAN" "$MYSQL0900_REPORT")"
mysql0900_import_exit="$(run_restore_attempt mysql84 mariadb10 "$MYSQL0900_DB" "$MYSQL0900_DUMP" "$MYSQL0900_IMPORT_LOG")"
mysql0900_client_probe_exit="$(run_client_probe mariadb10 mysql84 "SELECT @@collation_server, @@character_set_server;" "$MYSQL0900_CLIENT_PROBE" "$MYSQL0900_CLIENT_PROBE_ERR")"
mysql0900_unsupported_count="$(count_plan_code unsupported_destination_collation "$MYSQL0900_PLAN")"
mysql0900_client_risk_count="$(count_plan_code client_collation_compatibility_risk "$MYSQL0900_PLAN")"

cat >"$MYSQL0900_SUMMARY" <<JSON
{
  "scenario": "mysql84_to_mariadb10_utf8mb4_0900_ai_ci",
  "source_service": "mysql84",
  "dest_service": "mariadb10",
  "database": "$(json_escape "$MYSQL0900_DB")",
  "source_dsn": "$(json_escape "$(dsn_for_service mysql84)")",
  "dest_dsn": "$(json_escape "$(dsn_for_service mariadb10)")",
  "plan_exit_code": $mysql0900_plan_exit,
  "restore_exit_code": $mysql0900_import_exit,
  "unsupported_destination_count": ${mysql0900_unsupported_count:-0},
  "client_compatibility_risk_count": ${mysql0900_client_risk_count:-0},
  "representative_client_probe_exit_code": $mysql0900_client_probe_exit,
  "artifacts": {
    "source_fixture": "$(json_escape "$MYSQL0900_SOURCE_SQL")",
    "source_collations": "$(json_escape "$MYSQL0900_SOURCE_COLLATIONS")",
    "plan_output": "$(json_escape "$MYSQL0900_PLAN")",
    "report_output": "$(json_escape "$MYSQL0900_REPORT")",
    "dump_output": "$(json_escape "$MYSQL0900_DUMP")",
    "import_log": "$(json_escape "$MYSQL0900_IMPORT_LOG")",
    "representative_client_probe": "$(json_escape "$MYSQL0900_CLIENT_PROBE")",
    "representative_client_probe_stderr": "$(json_escape "$MYSQL0900_CLIENT_PROBE_ERR")"
  }
}
JSON

MARIADB_UCA1400_DB="phase63_mariadb_uca1400"
MARIADB_UCA1400_SOURCE_SQL="$MARIADB_UCA1400_TO_MYSQL_DIR/source-fixture.sql"
MARIADB_UCA1400_SOURCE_COLLATIONS="$MARIADB_UCA1400_TO_MYSQL_DIR/source-collations.tsv"
MARIADB_UCA1400_PLAN="$MARIADB_UCA1400_TO_MYSQL_DIR/plan-output.json"
MARIADB_UCA1400_REPORT="$MARIADB_UCA1400_TO_MYSQL_DIR/report-output.json"
MARIADB_UCA1400_DUMP="$MARIADB_UCA1400_TO_MYSQL_DIR/dump.sql"
MARIADB_UCA1400_IMPORT_LOG="$MARIADB_UCA1400_TO_MYSQL_DIR/import.stderr.log"
MARIADB_UCA1400_CLIENT_PROBE="$MARIADB_UCA1400_TO_MYSQL_DIR/client-probe.txt"
MARIADB_UCA1400_CLIENT_PROBE_ERR="$MARIADB_UCA1400_TO_MYSQL_DIR/client-probe.stderr.log"
MARIADB_UCA1400_SUMMARY="$MARIADB_UCA1400_TO_MYSQL_DIR/summary.json"

prepare_fixture "$MARIADB_UCA1400_FIXTURE" "$MARIADB_UCA1400_DB" "$MARIADB_UCA1400_SOURCE_SQL"
db_exec mariadb12 <"$MARIADB_UCA1400_SOURCE_SQL"
capture_collation_inventory mariadb12 "$MARIADB_UCA1400_DB" "$MARIADB_UCA1400_SOURCE_COLLATIONS"
mariadb_uca1400_plan_exit="$(run_plan_and_report mariadb12 mysql84 "$MARIADB_UCA1400_DB" "$MARIADB_UCA1400_TO_MYSQL_DIR/state" "$MARIADB_UCA1400_PLAN" "$MARIADB_UCA1400_REPORT")"
mariadb_uca1400_import_exit="$(run_restore_attempt mariadb12 mysql84 "$MARIADB_UCA1400_DB" "$MARIADB_UCA1400_DUMP" "$MARIADB_UCA1400_IMPORT_LOG")"
mariadb_uca1400_client_probe_exit="$(run_client_probe mysql80 mariadb12 "SELECT @@collation_server, @@character_set_server;" "$MARIADB_UCA1400_CLIENT_PROBE" "$MARIADB_UCA1400_CLIENT_PROBE_ERR")"
mariadb_uca1400_unsupported_count="$(count_plan_code unsupported_destination_collation "$MARIADB_UCA1400_PLAN")"
mariadb_uca1400_client_risk_count="$(count_plan_code client_collation_compatibility_risk "$MARIADB_UCA1400_PLAN")"

cat >"$MARIADB_UCA1400_SUMMARY" <<JSON
{
  "scenario": "mariadb12_to_mysql84_utf8mb4_uca1400_ai_ci",
  "source_service": "mariadb12",
  "dest_service": "mysql84",
  "database": "$(json_escape "$MARIADB_UCA1400_DB")",
  "source_dsn": "$(json_escape "$(dsn_for_service mariadb12)")",
  "dest_dsn": "$(json_escape "$(dsn_for_service mysql84)")",
  "plan_exit_code": $mariadb_uca1400_plan_exit,
  "restore_exit_code": $mariadb_uca1400_import_exit,
  "unsupported_destination_count": ${mariadb_uca1400_unsupported_count:-0},
  "client_compatibility_risk_count": ${mariadb_uca1400_client_risk_count:-0},
  "representative_client_probe_exit_code": $mariadb_uca1400_client_probe_exit,
  "artifacts": {
    "source_fixture": "$(json_escape "$MARIADB_UCA1400_SOURCE_SQL")",
    "source_collations": "$(json_escape "$MARIADB_UCA1400_SOURCE_COLLATIONS")",
    "plan_output": "$(json_escape "$MARIADB_UCA1400_PLAN")",
    "report_output": "$(json_escape "$MARIADB_UCA1400_REPORT")",
    "dump_output": "$(json_escape "$MARIADB_UCA1400_DUMP")",
    "import_log": "$(json_escape "$MARIADB_UCA1400_IMPORT_LOG")",
    "representative_client_probe": "$(json_escape "$MARIADB_UCA1400_CLIENT_PROBE")",
    "representative_client_probe_stderr": "$(json_escape "$MARIADB_UCA1400_CLIENT_PROBE_ERR")"
  }
}
JSON

MARIADB_UCA1400_RISK_SOURCE_SQL="$MARIADB_UCA1400_RISK_DIR/source-fixture.sql"
MARIADB_UCA1400_RISK_SOURCE_COLLATIONS="$MARIADB_UCA1400_RISK_DIR/source-collations.tsv"
MARIADB_UCA1400_RISK_PLAN="$MARIADB_UCA1400_RISK_DIR/plan-output.json"
MARIADB_UCA1400_RISK_REPORT="$MARIADB_UCA1400_RISK_DIR/report-output.json"
MARIADB_UCA1400_RISK_DUMP="$MARIADB_UCA1400_RISK_DIR/dump.sql"
MARIADB_UCA1400_RISK_IMPORT_LOG="$MARIADB_UCA1400_RISK_DIR/import.stderr.log"
MARIADB_UCA1400_RISK_CLIENT_PROBE="$MARIADB_UCA1400_RISK_DIR/client-probe.txt"
MARIADB_UCA1400_RISK_CLIENT_PROBE_ERR="$MARIADB_UCA1400_RISK_DIR/client-probe.stderr.log"
MARIADB_UCA1400_RISK_SUMMARY="$MARIADB_UCA1400_RISK_DIR/summary.json"

prepare_fixture "$MARIADB_UCA1400_FIXTURE" "$MARIADB_UCA1400_DB" "$MARIADB_UCA1400_RISK_SOURCE_SQL"
db_exec mariadb12 <"$MARIADB_UCA1400_RISK_SOURCE_SQL"
capture_collation_inventory mariadb12 "$MARIADB_UCA1400_DB" "$MARIADB_UCA1400_RISK_SOURCE_COLLATIONS"
mariadb_uca1400_risk_plan_exit="$(run_plan_and_report mariadb12 mariadb12 "$MARIADB_UCA1400_DB" "$MARIADB_UCA1400_RISK_DIR/state" "$MARIADB_UCA1400_RISK_PLAN" "$MARIADB_UCA1400_RISK_REPORT")"
mariadb_uca1400_risk_import_exit="$(run_restore_attempt mariadb12 mariadb12 "$MARIADB_UCA1400_DB" "$MARIADB_UCA1400_RISK_DUMP" "$MARIADB_UCA1400_RISK_IMPORT_LOG")"
mariadb_uca1400_risk_client_probe_exit="$(run_client_probe mysql80 mariadb12 "SELECT @@collation_server, @@character_set_server;" "$MARIADB_UCA1400_RISK_CLIENT_PROBE" "$MARIADB_UCA1400_RISK_CLIENT_PROBE_ERR")"
mariadb_uca1400_risk_unsupported_count="$(count_plan_code unsupported_destination_collation "$MARIADB_UCA1400_RISK_PLAN")"
mariadb_uca1400_risk_client_risk_count="$(count_plan_code client_collation_compatibility_risk "$MARIADB_UCA1400_RISK_PLAN")"

cat >"$MARIADB_UCA1400_RISK_SUMMARY" <<JSON
{
  "scenario": "mariadb12_to_mariadb12_utf8mb4_uca1400_ai_ci",
  "source_service": "mariadb12",
  "dest_service": "mariadb12",
  "database": "$(json_escape "$MARIADB_UCA1400_DB")",
  "source_dsn": "$(json_escape "$(dsn_for_service mariadb12)")",
  "dest_dsn": "$(json_escape "$(dsn_for_service mariadb12)")",
  "plan_exit_code": $mariadb_uca1400_risk_plan_exit,
  "restore_exit_code": $mariadb_uca1400_risk_import_exit,
  "unsupported_destination_count": ${mariadb_uca1400_risk_unsupported_count:-0},
  "client_compatibility_risk_count": ${mariadb_uca1400_risk_client_risk_count:-0},
  "representative_client_probe_exit_code": $mariadb_uca1400_risk_client_probe_exit,
  "artifacts": {
    "source_fixture": "$(json_escape "$MARIADB_UCA1400_RISK_SOURCE_SQL")",
    "source_collations": "$(json_escape "$MARIADB_UCA1400_RISK_SOURCE_COLLATIONS")",
    "plan_output": "$(json_escape "$MARIADB_UCA1400_RISK_PLAN")",
    "report_output": "$(json_escape "$MARIADB_UCA1400_RISK_REPORT")",
    "dump_output": "$(json_escape "$MARIADB_UCA1400_RISK_DUMP")",
    "import_log": "$(json_escape "$MARIADB_UCA1400_RISK_IMPORT_LOG")",
    "representative_client_probe": "$(json_escape "$MARIADB_UCA1400_RISK_CLIENT_PROBE")",
    "representative_client_probe_stderr": "$(json_escape "$MARIADB_UCA1400_RISK_CLIENT_PROBE_ERR")"
  }
}
JSON

cat >"$OUTPUT_DIR/summary.json" <<JSON
{
  "scenario": "phase63_collation_compatibility",
  "mysql84_to_mariadb10_utf8mb4_0900_ai_ci": "$(json_escape "$MYSQL0900_SUMMARY")",
  "mariadb12_to_mysql84_utf8mb4_uca1400_ai_ci": "$(json_escape "$MARIADB_UCA1400_SUMMARY")",
  "mariadb12_to_mariadb12_utf8mb4_uca1400_ai_ci": "$(json_escape "$MARIADB_UCA1400_RISK_SUMMARY")"
}
JSON

echo "Scenario complete. Summary: $OUTPUT_DIR/summary.json"
