#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

"$SCRIPT_DIR/run-migration-test.sh" "mariadb10" "mysql80" "$PROJECT_ROOT/configs/mariadb10-to-mysql80.yaml" "MariaDB 10.6 -> MySQL 8.0"
