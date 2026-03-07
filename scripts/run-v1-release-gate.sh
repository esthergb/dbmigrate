#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

MODE="minimal"
OUTPUT_ROOT=""

usage() {
  cat <<'USAGE'
usage: run-v1-release-gate.sh [--mode minimal|full] [--output-root <path>]

modes:
  minimal  run release build + unit tests + strict-lts smoke
  full     run minimal plus full strict-lts matrix and focused signoff rehearsals
USAGE
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

while [ "$#" -gt 0 ]; do
  case "$1" in
    --mode)
      MODE="${2:-}"
      shift 2
      ;;
    --output-root)
      OUTPUT_ROOT="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

case "$MODE" in
  minimal|full)
    ;;
  *)
    echo "invalid --mode value: $MODE (expected minimal or full)" >&2
    exit 1
    ;;
esac

if [ -z "$OUTPUT_ROOT" ]; then
  OUTPUT_ROOT="./state/v1-release-gate/$(date -u +%Y%m%dT%H%M%SZ)-${MODE}"
fi

OUTPUT_ROOT_ABS="$PROJECT_ROOT/${OUTPUT_ROOT#./}"
MANIFEST_TSV="$OUTPUT_ROOT_ABS/manifest.tsv"
SUMMARY_JSON="$OUTPUT_ROOT_ABS/summary.json"

mkdir -p "$OUTPUT_ROOT_ABS"
printf 'step\trc\tcompleted_at_utc\n' >"$MANIFEST_TSV"

run_step() {
  local step_name="$1"
  shift
  local rc
  set +e
  (
    cd "$PROJECT_ROOT"
    "$@"
  )
  rc=$?
  set -e
  printf '%s\t%s\t%s\n' "$step_name" "$rc" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >>"$MANIFEST_TSV"
  if [ "$rc" -ne 0 ]; then
    echo "step failed: $step_name (rc=$rc)" >&2
    return "$rc"
  fi
  return 0
}

run_step "build_release_binary" go build -trimpath -ldflags="-s -w" -o bin/dbmigrate ./cmd/dbmigrate
run_step "go_test_all" go test ./... -count=1
run_step "strict_lts_smoke_mysql84_to_mysql84" ./scripts/test-v1-mysql84-to-mysql84.sh

if [ "$MODE" = "full" ]; then
  run_step "strict_lts_full_matrix" ./scripts/test-v1-matrix.sh
  run_step "focused_signoff_rehearsals" ./scripts/run-v1-signoff-rehearsals.sh "$OUTPUT_ROOT_ABS/signoff-rehearsals"
fi

failed_steps="$(awk -F '\t' 'NR>1 && $2 != 0 {count++} END {print count+0}' "$MANIFEST_TSV")"
cat >"$SUMMARY_JSON" <<JSON
{
  "scenario": "v1_release_gate",
  "mode": "$(json_escape "$MODE")",
  "output_root": "$(json_escape "$OUTPUT_ROOT_ABS")",
  "manifest": "$(json_escape "$MANIFEST_TSV")",
  "failed_steps": $failed_steps,
  "generated_at_utc": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
JSON

echo "v1 release gate complete. Summary: $SUMMARY_JSON"
