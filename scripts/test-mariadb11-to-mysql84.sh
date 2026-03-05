#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

"$SCRIPT_DIR/run-migration-test.sh" "mariadb11" "mysql84" "$PROJECT_ROOT/configs/mariadb11-to-mysql84.yaml" "MariaDB 11.0 -> MySQL 8.4"
