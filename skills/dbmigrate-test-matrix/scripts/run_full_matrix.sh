#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../" && pwd)"
COMPOSE_FILE="$ROOT_DIR/docker-compose.yml"

if [[ "${1:-}" == "--help" ]]; then
  cat <<'HELP'
Usage: run_full_matrix.sh

Run full local validation matrix for dbmigrate.
Requires docker-compose.yml integration stack and tests.
HELP
  exit 0
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required" >&2
  exit 1
fi

if [[ ! -f "$COMPOSE_FILE" ]]; then
  echo "docker-compose.yml not found at $COMPOSE_FILE" >&2
  echo "Create project compose services before running full matrix." >&2
  exit 1
fi

cd "$ROOT_DIR"

mkdir -p logs

echo "[full] static checks"
gofmt -l .
go test ./... -count=1

echo "[full] validating compose"
docker compose -f "$COMPOSE_FILE" config >/dev/null

echo "[full] note: add per-pair integration commands as compose services are implemented"
echo "[full] completed"
