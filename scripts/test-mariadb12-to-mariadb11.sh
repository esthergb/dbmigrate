#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

"$SCRIPT_DIR/run-migration-test.sh" "mariadb12" "mariadb11" "$PROJECT_ROOT/configs/mariadb12-to-mariadb11.yaml" "MariaDB 12.0 -> MariaDB 11.0"
