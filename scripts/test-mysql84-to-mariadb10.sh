#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

"$SCRIPT_DIR/run-migration-test.sh" "mysql84" "mariadb10" "$PROJECT_ROOT/configs/mysql84-to-mariadb10.yaml" "MySQL 8.4 -> MariaDB 10.6"
