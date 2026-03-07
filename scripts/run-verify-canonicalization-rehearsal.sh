#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: $0 <output-dir>" >&2
  exit 1
fi

OUTPUT_DIR="$1"
SOURCE_SERVICE="${DBMIGRATE_VERIFY_CANONICAL_SOURCE_SERVICE:-mysql84a}"
DEST_SERVICE="${DBMIGRATE_VERIFY_CANONICAL_DEST_SERVICE:-mariadb114b}"
SOURCE_DB="phase64_verify"
DEST_DB="phase64_verify"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
SOURCE_FIXTURE_TEMPLATE="$PROJECT_ROOT/datasets/phase64_verify_source_mysql84.sql"
DEST_FIXTURE_TEMPLATE="$PROJECT_ROOT/datasets/phase64_verify_dest_mariadb12.sql"

compose() {
  docker compose -f "$PROJECT_ROOT/docker-compose.yml" "$@"
}

client_bin() {
  case "$1" in
    mariadb*) echo "mariadb" ;;
    *) echo "mysql" ;;
  esac
}

service_port() {
  case "$1" in
    mariadb12) echo "13308" ;;
    mariadb114a) echo "14411" ;;
    mariadb114b) echo "14412" ;;
    mariadb118a) echo "14811" ;;
    mariadb118b) echo "14812" ;;
    mysql84) echo "23307" ;;
    mysql84a) echo "24311" ;;
    mysql84b) echo "24312" ;;
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
  compose exec -T "$service" "$(client_bin "$service")" --default-character-set=utf8mb4 -u root -prootpass123 "$@"
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

hash_file() {
  shasum -a 256 "$1" | awk '{print $1}'
}

mkdir -p "$OUTPUT_DIR"

echo "Waiting for $SOURCE_SERVICE..."
wait_for_db "$SOURCE_SERVICE"
echo "Waiting for $DEST_SERVICE..."
wait_for_db "$DEST_SERVICE"

SOURCE_FIXTURE="$OUTPUT_DIR/source-fixture.sql"
DEST_FIXTURE="$OUTPUT_DIR/dest-fixture.sql"
sed "s/__DB__/$SOURCE_DB/g" "$SOURCE_FIXTURE_TEMPLATE" >"$SOURCE_FIXTURE"
sed "s/__DB__/$DEST_DB/g" "$DEST_FIXTURE_TEMPLATE" >"$DEST_FIXTURE"

db_exec "$SOURCE_SERVICE" <"$SOURCE_FIXTURE"
db_exec "$DEST_SERVICE" <"$DEST_FIXTURE"

SOURCE_COLLATION_TSV="$OUTPUT_DIR/source-collation-order.tsv"
DEST_COLLATION_TSV="$OUTPUT_DIR/dest-collation-order.tsv"
SOURCE_TEMPORAL_TSV="$OUTPUT_DIR/source-temporal-default-session.tsv"
DEST_TEMPORAL_TSV="$OUTPUT_DIR/dest-temporal-alt-session.tsv"
SOURCE_JSON_TSV="$OUTPUT_DIR/source-json.tsv"
DEST_JSON_TSV="$OUTPUT_DIR/dest-json.tsv"
SOURCE_COMBINED="$OUTPUT_DIR/source-naive-evidence.txt"
DEST_COMBINED="$OUTPUT_DIR/dest-naive-evidence.txt"
VERIFY_HASH_JSON="$OUTPUT_DIR/verify-hash.json"
VERIFY_SAMPLE_JSON="$OUTPUT_DIR/verify-sample.json"
VERIFY_FULL_JSON="$OUTPUT_DIR/verify-full-hash.json"
REPORT_JSON="$OUTPUT_DIR/report.json"
SUMMARY_JSON="$OUTPUT_DIR/summary.json"
STATE_DIR="$OUTPUT_DIR/state"

db_exec "$SOURCE_SERVICE" -N -B -e "USE \`$SOURCE_DB\`; SELECT label FROM collation_order_demo ORDER BY label;" >"$SOURCE_COLLATION_TSV"
db_exec "$DEST_SERVICE" -N -B -e "USE \`$DEST_DB\`; SELECT label FROM collation_order_demo ORDER BY label;" >"$DEST_COLLATION_TSV"
db_exec "$SOURCE_SERVICE" -N -B -e "USE \`$SOURCE_DB\`; SET SESSION time_zone = '+00:00'; SELECT id, ts_value, dt_value FROM temporal_demo ORDER BY id;" >"$SOURCE_TEMPORAL_TSV"
db_exec "$DEST_SERVICE" -N -B -e "USE \`$DEST_DB\`; SET SESSION time_zone = '+02:00'; SELECT id, ts_value, dt_value FROM temporal_demo ORDER BY id;" >"$DEST_TEMPORAL_TSV"
db_exec "$SOURCE_SERVICE" -N -B -e "USE \`$SOURCE_DB\`; SELECT id, payload FROM json_demo ORDER BY id;" >"$SOURCE_JSON_TSV"
db_exec "$DEST_SERVICE" -N -B -e "USE \`$DEST_DB\`; SELECT id, payload FROM json_demo ORDER BY id;" >"$DEST_JSON_TSV"

cat "$SOURCE_COLLATION_TSV" "$SOURCE_TEMPORAL_TSV" "$SOURCE_JSON_TSV" >"$SOURCE_COMBINED"
cat "$DEST_COLLATION_TSV" "$DEST_TEMPORAL_TSV" "$DEST_JSON_TSV" >"$DEST_COMBINED"

source_naive_hash="$(hash_file "$SOURCE_COMBINED")"
dest_naive_hash="$(hash_file "$DEST_COMBINED")"
naive_hashes_differ="false"
if [ "$source_naive_hash" != "$dest_naive_hash" ]; then
  naive_hashes_differ="true"
fi

set +e
"$PROJECT_ROOT/bin/dbmigrate" verify \
  --source "$(dsn_for_service "$SOURCE_SERVICE")" \
  --dest "$(dsn_for_service "$DEST_SERVICE")" \
  --databases "$SOURCE_DB" \
  --verify-level data \
  --data-mode hash \
  --state-dir "$STATE_DIR" \
  --json >"$VERIFY_HASH_JSON"
verify_hash_exit=$?

"$PROJECT_ROOT/bin/dbmigrate" verify \
  --source "$(dsn_for_service "$SOURCE_SERVICE")" \
  --dest "$(dsn_for_service "$DEST_SERVICE")" \
  --databases "$SOURCE_DB" \
  --verify-level data \
  --data-mode sample \
  --sample-size 2 \
  --state-dir "$STATE_DIR" \
  --json >"$VERIFY_SAMPLE_JSON"
verify_sample_exit=$?

"$PROJECT_ROOT/bin/dbmigrate" verify \
  --source "$(dsn_for_service "$SOURCE_SERVICE")" \
  --dest "$(dsn_for_service "$DEST_SERVICE")" \
  --databases "$SOURCE_DB" \
  --verify-level data \
  --data-mode full-hash \
  --state-dir "$STATE_DIR" \
  --json >"$VERIFY_FULL_JSON"
verify_full_exit=$?
set -e

"$PROJECT_ROOT/bin/dbmigrate" report \
  --state-dir "$STATE_DIR" \
  --json \
  --fail-on-conflict=false >"$REPORT_JSON"

risk_table_count="$( (rg -o '"representation_risk_tables": [0-9]+' "$VERIFY_HASH_JSON" || true) | awk '{print $2}' | tail -n 1)"
noise_risk_mismatch_count="$( (rg -o '"noise_risk_mismatches": [0-9]+' "$VERIFY_HASH_JSON" || true) | awk '{print $2}' | tail -n 1)"
if [ -z "${risk_table_count:-}" ]; then
  risk_table_count=0
fi
if [ -z "${noise_risk_mismatch_count:-}" ]; then
  noise_risk_mismatch_count=0
fi

cat >"$SUMMARY_JSON" <<JSON
{
  "scenario": "phase64_verify_canonicalization",
  "source_service": "$(json_escape "$SOURCE_SERVICE")",
  "dest_service": "$(json_escape "$DEST_SERVICE")",
  "source_dsn": "$(json_escape "$(dsn_for_service "$SOURCE_SERVICE")")",
  "dest_dsn": "$(json_escape "$(dsn_for_service "$DEST_SERVICE")")",
  "naive_hashes_differ": $naive_hashes_differ,
  "source_naive_hash": "$(json_escape "$source_naive_hash")",
  "dest_naive_hash": "$(json_escape "$dest_naive_hash")",
  "verify_hash_exit_code": $verify_hash_exit,
  "verify_sample_exit_code": $verify_sample_exit,
  "verify_full_hash_exit_code": $verify_full_exit,
  "representation_risk_tables": $risk_table_count,
  "noise_risk_mismatches": $noise_risk_mismatch_count,
  "artifacts": {
    "source_fixture": "$(json_escape "$SOURCE_FIXTURE")",
    "dest_fixture": "$(json_escape "$DEST_FIXTURE")",
    "source_collation_order": "$(json_escape "$SOURCE_COLLATION_TSV")",
    "dest_collation_order": "$(json_escape "$DEST_COLLATION_TSV")",
    "source_temporal": "$(json_escape "$SOURCE_TEMPORAL_TSV")",
    "dest_temporal": "$(json_escape "$DEST_TEMPORAL_TSV")",
    "source_json": "$(json_escape "$SOURCE_JSON_TSV")",
    "dest_json": "$(json_escape "$DEST_JSON_TSV")",
    "verify_hash": "$(json_escape "$VERIFY_HASH_JSON")",
    "verify_sample": "$(json_escape "$VERIFY_SAMPLE_JSON")",
    "verify_full_hash": "$(json_escape "$VERIFY_FULL_JSON")",
    "report": "$(json_escape "$REPORT_JSON")"
  }
}
JSON

echo "Scenario complete. Summary: $SUMMARY_JSON"
