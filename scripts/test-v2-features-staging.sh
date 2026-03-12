#!/usr/bin/env bash
# Staging test for v2 features: schema-objects, granular-verify, user/grant migration.
# Uses two MariaDB 10.11 containers (mariadb1011a -> mariadb1011b).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
BIN="$PROJECT_ROOT/bin/dbmigrate"

SOURCE_SVC="mariadb1011a"
DEST_SVC="mariadb1011b"
SRC_DSN="root:rootpass123@tcp(127.0.0.1:14311)/?"
DST_DSN="root:rootpass123@tcp(127.0.0.1:14312)/?"

PASS=0
FAIL=0
FAILURES=()

pass() { echo "  PASS: $1"; PASS=$((PASS+1)); }
fail() { echo "  FAIL: $1"; FAIL=$((FAIL+1)); FAILURES+=("$1"); }

compose() { docker compose -f "$PROJECT_ROOT/docker-compose.yml" "$@"; }

is_mariadb_service() {
  case "$1" in mariadb*) return 0 ;; *) return 1 ;; esac
}

wait_for_db() {
  local service="$1"
  local tries=90
  for _i in $(seq 1 "$tries"); do
    if is_mariadb_service "$service"; then
      if compose exec -T "$service" mariadb -u root -prootpass123 -e "SELECT 1" >/dev/null 2>&1; then
        return 0
      fi
    fi
    sleep 2
  done
  echo "database '$service' did not become healthy" >&2
  return 1
}

sql_src() { compose exec -T "$SOURCE_SVC" mariadb -u root -prootpass123 "$@"; }
sql_dst() { compose exec -T "$DEST_SVC"   mariadb -u root -prootpass123 "$@"; }

echo "================================================"
echo "v2 Feature Staging: schema-objects, granular-verify, migrate-users"
echo "================================================"

echo "[1/8] Resetting containers..."
compose down -v --remove-orphans >/dev/null 2>&1 || true

echo "[2/8] Starting containers..."
compose up -d "$SOURCE_SVC" "$DEST_SVC"

echo "[3/8] Waiting for health..."
wait_for_db "$SOURCE_SVC"
wait_for_db "$DEST_SVC"

echo "[4/8] Building dbmigrate..."
(cd "$PROJECT_ROOT" && make build)

echo "[5/8] Seeding source with routines, triggers, events, users..."

sql_src -e "CREATE DATABASE IF NOT EXISTS testdb;"

sql_src testdb -e "
  CREATE TABLE IF NOT EXISTS orders (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    customer_id BIGINT NOT NULL,
    amount DECIMAL(10,2) NOT NULL,
    created_at DATETIME DEFAULT NOW()
  );
  CREATE TABLE IF NOT EXISTS order_audit (
    id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    order_id BIGINT NOT NULL,
    action VARCHAR(32) NOT NULL,
    ts DATETIME DEFAULT NOW()
  );
  INSERT INTO orders (customer_id, amount) VALUES (1, 100.00), (2, 200.00);
"

sql_src testdb <<'DELIM'
DROP PROCEDURE IF EXISTS sp_get_orders;
DELIMITER //
CREATE PROCEDURE sp_get_orders(IN p_customer BIGINT)
BEGIN
  SELECT * FROM orders WHERE customer_id = p_customer;
END //
DELIMITER ;
DELIM

sql_src testdb <<'DELIM'
DROP FUNCTION IF EXISTS fn_total_orders;
DELIMITER //
CREATE FUNCTION fn_total_orders(p_customer BIGINT) RETURNS INT DETERMINISTIC
BEGIN
  DECLARE cnt INT;
  SELECT COUNT(*) INTO cnt FROM orders WHERE customer_id = p_customer;
  RETURN cnt;
END //
DELIMITER ;
DELIM

sql_src testdb <<'DELIM'
DROP TRIGGER IF EXISTS trg_order_insert;
DELIMITER //
CREATE TRIGGER trg_order_insert AFTER INSERT ON orders
FOR EACH ROW
BEGIN
  INSERT INTO order_audit (order_id, action) VALUES (NEW.id, 'INSERT');
END //
DELIMITER ;
DELIM

sql_src testdb -e "DROP EVENT IF EXISTS ev_cleanup; CREATE EVENT ev_cleanup ON SCHEDULE EVERY 1 HOUR DO DELETE FROM order_audit WHERE ts < NOW() - INTERVAL 30 DAY;"

echo "[5b/8] Creating test user on source..."
sql_src -e "DROP USER IF EXISTS 'migtest'@'%'; CREATE USER 'migtest'@'%' IDENTIFIED BY 'testpass123'; GRANT SELECT ON testdb.* TO 'migtest'@'%'; FLUSH PRIVILEGES;" 2>/dev/null || true

echo "[6/8] Running schema migration with routines+triggers+events..."
set +e

"$BIN" migrate \
  --source "$SRC_DSN" \
  --dest   "$DST_DSN" \
  --tls-mode disabled \
  --include-objects "tables,views,routines,triggers,events" \
  --schema-only \
  --force
schema_rc=$?
set -e

if [ "$schema_rc" -eq 0 ]; then
  pass "schema migrate with routines/triggers/events"
else
  fail "schema migrate with routines/triggers/events (exit=$schema_rc)"
fi

echo "[7/8] Verifying schema objects..."

verify_schema_rc=0
set +e
"$BIN" verify \
  --source "$SRC_DSN" \
  --dest   "$DST_DSN" \
  --tls-mode disabled \
  --include-objects "tables,views,routines,triggers,events" \
  --verify-level schema
verify_schema_rc=$?
set -e
if [ "$verify_schema_rc" -eq 0 ]; then
  pass "verify schema (tables+views+routines+triggers+events)"
else
  fail "verify schema (exit=$verify_schema_rc)"
fi

verify_granular_rc=0
set +e
"$BIN" verify \
  --source "$SRC_DSN" \
  --dest   "$DST_DSN" \
  --tls-mode disabled \
  --include-objects "tables" \
  --verify-level schema-granular
verify_granular_rc=$?
set -e
if [ "$verify_granular_rc" -eq 0 ]; then
  pass "verify schema-granular (column/index/FK/partition)"
else
  fail "verify schema-granular (exit=$verify_granular_rc)"
fi

echo "[8/8] Testing migrate-users dry-run..."
users_dryrun_rc=0
set +e
"$BIN" migrate-users \
  --source "$SRC_DSN" \
  --dest   "$DST_DSN" \
  --tls-mode disabled \
  --dry-run \
  --scope business
users_dryrun_rc=$?
set -e
if [ "$users_dryrun_rc" -eq 0 ]; then
  pass "migrate-users dry-run"
else
  fail "migrate-users dry-run (exit=$users_dryrun_rc)"
fi

echo "[8b/8] Testing migrate-users live..."
users_live_rc=0
set +e
"$BIN" migrate-users \
  --source "$SRC_DSN" \
  --dest   "$DST_DSN" \
  --tls-mode disabled \
  --scope business \
  --skip-locked
users_live_rc=$?
set -e
if [ "$users_live_rc" -eq 0 ]; then
  pass "migrate-users live (business scope)"
else
  fail "migrate-users live (exit=$users_live_rc)"
fi

echo ""
echo "================================================"
echo "Results: $PASS passed, $FAIL failed"
if [ "${#FAILURES[@]}" -gt 0 ]; then
  echo "Failed tests:"
  for f in "${FAILURES[@]}"; do
    echo "  - $f"
  done
fi
echo "================================================"

[ "$FAIL" -eq 0 ]
