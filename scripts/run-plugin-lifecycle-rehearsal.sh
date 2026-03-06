#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 3 ]; then
  echo "usage: $0 <source-service> <dest-service> <output-dir>" >&2
  exit 1
fi

SOURCE_SERVICE="$1"
DEST_SERVICE="$2"
OUTPUT_DIR="$3"
SCHEMA_NAME="phase60_plugin_lifecycle"

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
  if is_mariadb_service "$1"; then
    echo "mariadb"
  else
    echo "mysql"
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

mkdir -p "$OUTPUT_DIR"

SOURCE_ACCOUNTS_FILE="$OUTPUT_DIR/source-accounts.tsv"
SOURCE_ENGINES_FILE="$OUTPUT_DIR/source-table-engines.tsv"
DEST_PLUGINS_FILE="$OUTPUT_DIR/dest-plugins.tsv"
DEST_ENGINES_FILE="$OUTPUT_DIR/dest-engines.tsv"
DEST_DEFAULT_AUTH_FILE="$OUTPUT_DIR/dest-default-auth.txt"
PLAN_OUTPUT_FILE="$OUTPUT_DIR/plan-output.json"
SUMMARY_FILE="$OUTPUT_DIR/summary.json"

echo "Waiting for $SOURCE_SERVICE..."
wait_for_db "$SOURCE_SERVICE"
echo "Waiting for $DEST_SERVICE..."
wait_for_db "$DEST_SERVICE"

echo "Preparing source fixtures..."
db_exec "$SOURCE_SERVICE" >/dev/null 2>&1 <<SQL
DROP DATABASE IF EXISTS \`$SCHEMA_NAME\`;
CREATE DATABASE \`$SCHEMA_NAME\`;
USE \`$SCHEMA_NAME\`;
CREATE TABLE \`core_items\` (
  item_id INT PRIMARY KEY,
  note VARCHAR(64) NOT NULL
) ENGINE=InnoDB;
INSERT INTO \`core_items\` (item_id, note) VALUES (1, 'phase60');
DROP USER IF EXISTS 'phase60_default'@'%';
CREATE USER 'phase60_default'@'%' IDENTIFIED BY 'Phase60#123456!';
SQL

if is_mariadb_service "$SOURCE_SERVICE"; then
  db_exec "$SOURCE_SERVICE" >/dev/null 2>&1 <<SQL
USE \`$SCHEMA_NAME\`;
CREATE TABLE \`aria_items\` (
  item_id INT PRIMARY KEY,
  note VARCHAR(64) NOT NULL
) ENGINE=Aria;
INSERT INTO \`aria_items\` (item_id, note) VALUES (1, 'aria-only');
SQL
fi

case "$SOURCE_SERVICE" in
  mysql80)
    db_exec "$SOURCE_SERVICE" >/dev/null 2>&1 <<'SQL'
DROP USER IF EXISTS 'phase60_native'@'%';
CREATE USER 'phase60_native'@'%' IDENTIFIED WITH mysql_native_password BY 'Phase60#123456!';
SQL
    ;;
  mysql84)
    db_exec "$SOURCE_SERVICE" >/dev/null 2>&1 <<'SQL'
DROP USER IF EXISTS 'phase60_caching'@'%';
CREATE USER 'phase60_caching'@'%' IDENTIFIED WITH caching_sha2_password BY 'Phase60#123456!';
SQL
    ;;
esac

echo "Capturing source inventories..."
db_exec "$SOURCE_SERVICE" -N -B -e "SELECT User, Host, plugin FROM mysql.user WHERE User LIKE 'phase60_%' ORDER BY User, Host;" >"$SOURCE_ACCOUNTS_FILE"
db_exec "$SOURCE_SERVICE" -N -B -e "SELECT TABLE_SCHEMA, TABLE_NAME, ENGINE FROM information_schema.TABLES WHERE TABLE_SCHEMA = '$SCHEMA_NAME' AND TABLE_TYPE = 'BASE TABLE' ORDER BY TABLE_NAME;" >"$SOURCE_ENGINES_FILE"

echo "Capturing destination inventories..."
db_exec "$DEST_SERVICE" -N -B -e "SELECT PLUGIN_NAME, PLUGIN_STATUS FROM information_schema.PLUGINS WHERE PLUGIN_STATUS IN ('ACTIVE', 'ENABLED') ORDER BY PLUGIN_NAME;" >"$DEST_PLUGINS_FILE"
db_exec "$DEST_SERVICE" -N -B -e "SELECT ENGINE, SUPPORT FROM information_schema.ENGINES ORDER BY ENGINE;" >"$DEST_ENGINES_FILE"
if db_exec "$DEST_SERVICE" -N -B -e "SELECT @@default_authentication_plugin;" >"$DEST_DEFAULT_AUTH_FILE" 2>/dev/null; then
  :
else
  echo "unavailable" >"$DEST_DEFAULT_AUTH_FILE"
fi

echo "Running dbmigrate plan..."
set +e
"$PROJECT_ROOT/bin/dbmigrate" plan \
  --source "$(dsn_for_service "$SOURCE_SERVICE")" \
  --dest "$(dsn_for_service "$DEST_SERVICE")" \
  --databases "$SCHEMA_NAME" \
  --downgrade-profile max-compat \
  --json >"$PLAN_OUTPUT_FILE"
plan_exit_code=$?
set -e

unsupported_auth_count="$(rg -c '"code": "unsupported_auth_plugin_account"' "$PLAN_OUTPUT_FILE" || true)"
unsupported_engine_count="$(rg -c '"code": "unsupported_storage_engine_table"' "$PLAN_OUTPUT_FILE" || true)"
auth_detected="false"
engine_detected="false"
if [ "${unsupported_auth_count:-0}" -gt 0 ]; then
  auth_detected="true"
fi
if [ "${unsupported_engine_count:-0}" -gt 0 ]; then
  engine_detected="true"
fi

cat >"$SUMMARY_FILE" <<JSON
{
  "scenario": "plugin_lifecycle_and_disabled_feature_flags",
  "source_service": "$(json_escape "$SOURCE_SERVICE")",
  "dest_service": "$(json_escape "$DEST_SERVICE")",
  "source_dsn": "$(json_escape "$(dsn_for_service "$SOURCE_SERVICE")")",
  "dest_dsn": "$(json_escape "$(dsn_for_service "$DEST_SERVICE")")",
  "plan_exit_code": $plan_exit_code,
  "unsupported_auth_plugins_detected": $auth_detected,
  "unsupported_storage_engines_detected": $engine_detected,
  "unsupported_auth_plugin_count": ${unsupported_auth_count:-0},
  "unsupported_storage_engine_count": ${unsupported_engine_count:-0},
  "artifacts": {
    "source_accounts": "$(json_escape "$SOURCE_ACCOUNTS_FILE")",
    "source_table_engines": "$(json_escape "$SOURCE_ENGINES_FILE")",
    "dest_plugins": "$(json_escape "$DEST_PLUGINS_FILE")",
    "dest_engines": "$(json_escape "$DEST_ENGINES_FILE")",
    "dest_default_auth": "$(json_escape "$DEST_DEFAULT_AUTH_FILE")",
    "plan_output": "$(json_escape "$PLAN_OUTPUT_FILE")"
  }
}
JSON

echo "Scenario complete. Summary: $SUMMARY_FILE"
