#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

"$SCRIPT_DIR/run-migration-test.sh" "mysql84a" "mariadb114b" "$PROJECT_ROOT/configs/v1-mysql84a-to-mariadb114b.yaml" "v1 strict-lts: MySQL 8.4 -> MariaDB 11.4"
