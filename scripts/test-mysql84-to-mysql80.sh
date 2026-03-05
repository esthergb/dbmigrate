#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

"$SCRIPT_DIR/run-migration-test.sh" "mysql84" "mysql80" "$PROJECT_ROOT/configs/mysql84-to-mysql80.yaml" "MySQL 8.4 -> MySQL 8.0"
