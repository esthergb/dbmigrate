#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 4 ]; then
  echo "usage: $0 <source-service> <dest-service> <config-file> <label>" >&2
  exit 1
fi

SOURCE_SERVICE="$1"
DEST_SERVICE="$2"
CONFIG_FILE="$3"
LABEL="$4"

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

wait_for_db() {
  local service="$1"
  local tries=90
  local i
  for i in $(seq 1 "$tries"); do
    if is_mariadb_service "$service"; then
      if compose exec -T "$service" mariadb -u root -prootpass123 -e "SELECT 1" >/dev/null 2>&1; then
        return 0
      fi
    else
      if compose exec -T "$service" mysql -u root -prootpass123 -e "SELECT 1" >/dev/null 2>&1; then
        return 0
      fi
    fi
    sleep 2
  done
  echo "database service '$service' did not become healthy in time" >&2
  return 1
}

wait_for_db_health() {
  local service="$1"
  if wait_for_db "$service"; then
    return 0
  fi
  echo "recent logs for $service:" >&2
  compose logs --no-color --tail 80 "$service" >&2 || true
  return 1
}

dataset_for_service() {
  case "$1" in
    mariadb10) echo "$PROJECT_ROOT/datasets/populate_mariadb10.sql" ;;
    mariadb11) echo "$PROJECT_ROOT/datasets/populate_mariadb11.sql" ;;
    mariadb12) echo "$PROJECT_ROOT/datasets/populate_mariadb12.sql" ;;
    mysql80) echo "$PROJECT_ROOT/datasets/populate_mysql80.sql" ;;
    mysql84) echo "$PROJECT_ROOT/datasets/populate_mysql84.sql" ;;
    *) echo "unknown service '$1'" >&2; return 1 ;;
  esac
}

seed_source_dataset() {
  local service="$1"
  local dataset="$2"
  if is_mariadb_service "$service"; then
    compose exec -T "$service" mariadb -u root -prootpass123 < "$dataset"
  else
    compose exec -T "$service" mysql -u root -prootpass123 < "$dataset"
  fi
}

state_dir_for_config() {
  awk -F': ' '/^state-dir:/{print $2; exit}' "$1"
}

echo "=========================================="
echo "Testing: $LABEL"
echo "=========================================="

echo "[1/6] Resetting containers and volumes..."
compose down -v --remove-orphans >/dev/null 2>&1 || true

echo "[2/6] Starting source and destination containers..."
compose up -d "$SOURCE_SERVICE" "$DEST_SERVICE"

echo "[3/6] Waiting for database health checks..."
wait_for_db_health "$SOURCE_SERVICE"
wait_for_db_health "$DEST_SERVICE"

echo "[4/6] Preparing source dataset and local state..."
DATASET_FILE="$(dataset_for_service "$SOURCE_SERVICE")"
if [ ! -f "$DATASET_FILE" ]; then
  echo "dataset file not found: $DATASET_FILE" >&2
  exit 1
fi
seed_source_dataset "$SOURCE_SERVICE" "$DATASET_FILE"

STATE_DIR_RAW="$(state_dir_for_config "$CONFIG_FILE")"
if [ -n "$STATE_DIR_RAW" ]; then
  STATE_DIR="$PROJECT_ROOT/${STATE_DIR_RAW#./}"
  rm -rf "$STATE_DIR"
fi

if [ ! -f "$PROJECT_ROOT/bin/dbmigrate" ]; then
  echo "[5/6] Building dbmigrate..."
  (cd "$PROJECT_ROOT" && make build)
else
  echo "[5/6] dbmigrate binary found, skipping build"
fi

echo "[6/6] Running migration pipeline..."
first_failure=0
plan_rc=0
migrate_rc=0

set +e
(cd "$PROJECT_ROOT" && ./bin/dbmigrate plan --config "$CONFIG_FILE")
plan_rc=$?
set -e
if [ "$plan_rc" -ne 0 ] && [ "$first_failure" -eq 0 ]; then
  first_failure="$plan_rc"
fi

if [ "$plan_rc" -eq 0 ]; then
  set +e
  (cd "$PROJECT_ROOT" && ./bin/dbmigrate migrate --config "$CONFIG_FILE")
  migrate_rc=$?
  set -e
  if [ "$migrate_rc" -ne 0 ] && [ "$first_failure" -eq 0 ]; then
    first_failure="$migrate_rc"
  fi
else
  echo "Skipping migrate because plan failed with exit code $plan_rc"
fi

if [ "$migrate_rc" -eq 0 ] && [ "$plan_rc" -eq 0 ]; then
  set +e
  (cd "$PROJECT_ROOT" && ./bin/dbmigrate verify --config "$CONFIG_FILE" --verify-level data --data-mode count)
  verify_rc=$?
  set -e
  if [ "$verify_rc" -ne 0 ] && [ "$first_failure" -eq 0 ]; then
    first_failure="$verify_rc"
  fi
else
  echo "Skipping verify because migrate failed or was skipped"
fi

set +e
(cd "$PROJECT_ROOT" && ./bin/dbmigrate report --config "$CONFIG_FILE" --json --fail-on-conflict=false)
report_rc=$?
set -e
if [ "$report_rc" -ne 0 ] && [ "$first_failure" -eq 0 ]; then
  first_failure="$report_rc"
fi

if [ "$first_failure" -eq 0 ]; then
  echo "Test completed successfully: $LABEL"
  exit 0
fi

echo "Test finished with failure (exit=$first_failure): $LABEL"
exit "$first_failure"
