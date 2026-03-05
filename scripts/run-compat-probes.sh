#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: $0 <service> <output-json-file>" >&2
  exit 1
fi

SERVICE="$1"
OUT_FILE="$2"
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

db_client_for_service() {
  if is_mariadb_service "$1"; then
    echo "mariadb"
  else
    echo "mysql"
  fi
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

run_sql() {
  local service="$1"
  local sql="$2"
  local client
  client="$(db_client_for_service "$service")"
  compose exec -T "$service" "$client" -u root -prootpass123 -N -B -e "$sql"
}

probe_sql() {
  local probe_id="$1"
  case "$probe_id" in
    set_persist)
      cat <<'EOF'
SET PERSIST max_connections = @@GLOBAL.max_connections;
EOF
      ;;
    json_arrow_operators)
      cat <<'EOF'
DROP DATABASE IF EXISTS dbmigrate_probe;
CREATE DATABASE dbmigrate_probe;
USE dbmigrate_probe;
CREATE TABLE probe_json_ops (doc JSON);
INSERT INTO probe_json_ops(doc) VALUES ('{"a": 1}');
SELECT doc->'$.a' AS json_arrow, doc->>'$.a' AS json_arrow_unquote FROM probe_json_ops;
EOF
      ;;
    lateral_derived_table)
      cat <<'EOF'
DROP DATABASE IF EXISTS dbmigrate_probe;
CREATE DATABASE dbmigrate_probe;
USE dbmigrate_probe;
CREATE TABLE probe_parent(id INT PRIMARY KEY, v INT);
INSERT INTO probe_parent(id, v) VALUES (1, 10);
SELECT p.id, d.sum_v
FROM probe_parent p,
LATERAL (SELECT p.v + 1 AS sum_v) d;
EOF
      ;;
    max_execution_time_variable)
      cat <<'EOF'
SELECT @@max_execution_time AS max_execution_time;
EOF
      ;;
    max_statement_time_variable)
      cat <<'EOF'
SELECT @@max_statement_time AS max_statement_time;
EOF
      ;;
    set_session_authorization)
      cat <<'EOF'
SET SESSION AUTHORIZATION 'root'@'localhost';
EOF
      ;;
    restrict_fk_on_non_standard_key_variable)
      cat <<'EOF'
SELECT @@restrict_fk_on_non_standard_key AS restrict_fk_on_non_standard_key;
EOF
      ;;
    nonstandard_fk_reference)
      cat <<'EOF'
DROP DATABASE IF EXISTS dbmigrate_probe;
CREATE DATABASE dbmigrate_probe;
USE dbmigrate_probe;
CREATE TABLE parent_ns_fk (
  id INT PRIMARY KEY AUTO_INCREMENT,
  nonuniq INT,
  INDEX idx_nonuniq (nonuniq)
);
CREATE TABLE child_ns_fk (
  id INT PRIMARY KEY AUTO_INCREMENT,
  parent_nonuniq INT,
  CONSTRAINT fk_ns FOREIGN KEY (parent_nonuniq) REFERENCES parent_ns_fk(nonuniq)
);
EOF
      ;;
    default_authentication_plugin_variable)
      cat <<'EOF'
SELECT @@default_authentication_plugin AS default_authentication_plugin;
EOF
      ;;
    mysql_native_password_plugin)
      cat <<'EOF'
DROP USER IF EXISTS 'dbmigrate_probe_native'@'localhost';
CREATE USER 'dbmigrate_probe_native'@'localhost' IDENTIFIED WITH mysql_native_password BY 'Probe#123456!';
DROP USER IF EXISTS 'dbmigrate_probe_native'@'localhost';
EOF
      ;;
    server_character_set)
      cat <<'EOF'
SHOW VARIABLES LIKE 'character_set_server';
EOF
      ;;
    server_collation)
      cat <<'EOF'
SHOW VARIABLES LIKE 'collation_server';
EOF
      ;;
    session_sql_mode)
      cat <<'EOF'
SELECT @@SESSION.sql_mode AS session_sql_mode;
EOF
      ;;
    zero_datetime_default_strict)
      cat <<'EOF'
DROP DATABASE IF EXISTS dbmigrate_probe;
CREATE DATABASE dbmigrate_probe;
USE dbmigrate_probe;
CREATE TABLE probe_zero_datetime_default (
  dt DATETIME NOT NULL DEFAULT '0000-00-00 00:00:00'
);
EOF
      ;;
    zero_timestamp_default_strict)
      cat <<'EOF'
DROP DATABASE IF EXISTS dbmigrate_probe;
CREATE DATABASE dbmigrate_probe;
USE dbmigrate_probe;
CREATE TABLE probe_zero_timestamp_default (
  ts TIMESTAMP NOT NULL DEFAULT '0000-00-00 00:00:00'
);
EOF
      ;;
    zero_date_default_strict)
      cat <<'EOF'
DROP DATABASE IF EXISTS dbmigrate_probe;
CREATE DATABASE dbmigrate_probe;
USE dbmigrate_probe;
CREATE TABLE probe_zero_date_default (
  d DATE NOT NULL DEFAULT '0000-00-00'
);
EOF
      ;;
    zero_in_date_default_strict)
      cat <<'EOF'
DROP DATABASE IF EXISTS dbmigrate_probe;
CREATE DATABASE dbmigrate_probe;
USE dbmigrate_probe;
CREATE TABLE probe_zero_in_date_default (
  d DATE NOT NULL DEFAULT '2020-00-15'
);
EOF
      ;;
    invalid_calendar_date_default)
      cat <<'EOF'
DROP DATABASE IF EXISTS dbmigrate_probe;
CREATE DATABASE dbmigrate_probe;
USE dbmigrate_probe;
CREATE TABLE probe_invalid_calendar_date_default (
  d DATE NOT NULL DEFAULT '2024-02-30'
);
EOF
      ;;
    invalid_calendar_datetime_default)
      cat <<'EOF'
DROP DATABASE IF EXISTS dbmigrate_probe;
CREATE DATABASE dbmigrate_probe;
USE dbmigrate_probe;
CREATE TABLE probe_invalid_calendar_datetime_default (
  dt DATETIME NOT NULL DEFAULT '2024-02-30 01:02:03'
);
EOF
      ;;
    timestamp_out_of_range_default)
      cat <<'EOF'
DROP DATABASE IF EXISTS dbmigrate_probe;
CREATE DATABASE dbmigrate_probe;
USE dbmigrate_probe;
CREATE TABLE probe_timestamp_out_of_range_default (
  ts TIMESTAMP NOT NULL DEFAULT '1969-12-31 23:59:59'
);
EOF
      ;;
    *)
      echo "unknown probe_id: $probe_id" >&2
      return 1
      ;;
  esac
}

probe_description() {
  local probe_id="$1"
  case "$probe_id" in
    set_persist) echo "Checks support for SET PERSIST (documented as MySQL-only)." ;;
    json_arrow_operators) echo "Checks MySQL JSON -> / ->> operators." ;;
    lateral_derived_table) echo "Checks support for LATERAL derived tables." ;;
    max_execution_time_variable) echo "Checks MySQL max_execution_time variable." ;;
    max_statement_time_variable) echo "Checks MariaDB max_statement_time variable." ;;
    set_session_authorization) echo "Checks support for SET SESSION AUTHORIZATION (MariaDB 12+)." ;;
    restrict_fk_on_non_standard_key_variable) echo "Checks MySQL 8.4 restrict_fk_on_non_standard_key variable." ;;
    nonstandard_fk_reference) echo "Checks FK reference to non-unique indexed parent key." ;;
    default_authentication_plugin_variable) echo "Checks default_authentication_plugin variable (removed in MySQL 8.4)." ;;
    mysql_native_password_plugin) echo "Checks mysql_native_password plugin usability." ;;
    server_character_set) echo "Captures server default character_set_server." ;;
    server_collation) echo "Captures server default collation_server." ;;
    session_sql_mode) echo "Captures session sql_mode for strict/zero-date diagnostics." ;;
    zero_datetime_default_strict) echo "Checks strict-mode acceptance of DATETIME zero default (0000-00-00 00:00:00)." ;;
    zero_timestamp_default_strict) echo "Checks strict-mode acceptance of TIMESTAMP zero default (0000-00-00 00:00:00)." ;;
    zero_date_default_strict) echo "Checks strict-mode acceptance of DATE zero default (0000-00-00)." ;;
    zero_in_date_default_strict) echo "Checks strict-mode acceptance of DATE with zero month component (YYYY-00-DD)." ;;
    invalid_calendar_date_default) echo "Checks acceptance of invalid calendar DATE default (YYYY-02-30)." ;;
    invalid_calendar_datetime_default) echo "Checks acceptance of invalid calendar DATETIME default (YYYY-02-30 HH:MM:SS)." ;;
    timestamp_out_of_range_default) echo "Checks TIMESTAMP default lower-bound enforcement (pre-1970)." ;;
    *) echo "Unknown probe." ;;
  esac
}

PROBE_IDS=(
  set_persist
  json_arrow_operators
  lateral_derived_table
  max_execution_time_variable
  max_statement_time_variable
  set_session_authorization
  restrict_fk_on_non_standard_key_variable
  nonstandard_fk_reference
  default_authentication_plugin_variable
  mysql_native_password_plugin
  server_character_set
  server_collation
  session_sql_mode
  zero_datetime_default_strict
  zero_timestamp_default_strict
  zero_date_default_strict
  zero_in_date_default_strict
  invalid_calendar_date_default
  invalid_calendar_datetime_default
  timestamp_out_of_range_default
)

mkdir -p "$(dirname "$OUT_FILE")"

version_row="$(run_sql "$SERVICE" "SELECT @@version, @@version_comment;")"
version="$(printf '%s' "$version_row" | awk -F'\t' 'NR==1{print $1}')"
version_comment="$(printf '%s' "$version_row" | awk -F'\t' 'NR==1{print $2}')"

ok_count=0
failed_count=0
first_result=1

{
  echo "{"
  echo "  \"service\": \"$(json_escape "$SERVICE")\","
  echo "  \"engine_family\": \"$(json_escape "$(is_mariadb_service "$SERVICE" && echo mariadb || echo mysql)")\","
  echo "  \"version\": \"$(json_escape "$version")\","
  echo "  \"version_comment\": \"$(json_escape "$version_comment")\","
  echo "  \"generated_at_utc\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\","
  echo "  \"results\": ["
} > "$OUT_FILE"

for probe_id in "${PROBE_IDS[@]}"; do
  sql="$(probe_sql "$probe_id")"
  description="$(probe_description "$probe_id")"

  tmp_out="$(mktemp)"
  tmp_err="$(mktemp)"
  set +e
  run_sql "$SERVICE" "$sql" >"$tmp_out" 2>"$tmp_err"
  rc=$?
  set -e

  stdout_content="$(cat "$tmp_out")"
  stderr_content="$(cat "$tmp_err")"
  rm -f "$tmp_out" "$tmp_err"

  status="ok"
  if [ "$rc" -ne 0 ]; then
    status="failed"
    failed_count=$((failed_count + 1))
  else
    ok_count=$((ok_count + 1))
  fi

  if [ "$first_result" -eq 0 ]; then
    echo "," >> "$OUT_FILE"
  fi
  first_result=0

  {
    echo "    {"
    echo "      \"probe_id\": \"$(json_escape "$probe_id")\","
    echo "      \"description\": \"$(json_escape "$description")\","
    echo "      \"status\": \"$(json_escape "$status")\","
    echo "      \"exit_code\": $rc,"
    echo "      \"stdout\": \"$(json_escape "$stdout_content")\","
    echo "      \"stderr\": \"$(json_escape "$stderr_content")\""
    echo -n "    }"
  } >> "$OUT_FILE"
done

# Best-effort cleanup for probe side effects.
run_sql "$SERVICE" "DROP DATABASE IF EXISTS dbmigrate_probe;" >/dev/null 2>&1 || true
run_sql "$SERVICE" "DROP USER IF EXISTS 'dbmigrate_probe_native'@'localhost';" >/dev/null 2>&1 || true

{
  echo
  echo "  ],"
  echo "  \"probe_count\": ${#PROBE_IDS[@]},"
  echo "  \"ok\": $ok_count,"
  echo "  \"failed\": $failed_count"
  echo "}"
} >> "$OUT_FILE"

echo "[compat-probes] service=$SERVICE version=$version probes=${#PROBE_IDS[@]} ok=$ok_count failed=$failed_count output=$OUT_FILE"
