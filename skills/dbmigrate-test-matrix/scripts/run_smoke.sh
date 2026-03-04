#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../" && pwd)"
COMPOSE_FILE="$ROOT_DIR/docker-compose.yml"

if [[ "${1:-}" == "--help" ]]; then
  cat <<'HELP'
Usage: run_smoke.sh

Run quick local smoke checks for dbmigrate.
Expected to be used after docker-compose.yml and basic tests exist.
HELP
  exit 0
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required" >&2
  exit 1
fi

if [[ ! -f "$COMPOSE_FILE" ]]; then
  echo "docker-compose.yml not found at $COMPOSE_FILE" >&2
  echo "Create project compose services before running smoke checks." >&2
  exit 1
fi

cd "$ROOT_DIR"

echo "[smoke] formatting check"
gofmt -l .

echo "[smoke] unit tests"
go test ./... -count=1

echo "[smoke] docker compose config validation"
docker compose -f "$COMPOSE_FILE" config >/dev/null

echo "[smoke] completed"
