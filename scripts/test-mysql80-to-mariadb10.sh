#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

"$SCRIPT_DIR/run-migration-test.sh" "mysql80" "mariadb10" "$PROJECT_ROOT/configs/mysql80-to-mariadb10.yaml" "MySQL 8.0 -> MariaDB 10.6"
