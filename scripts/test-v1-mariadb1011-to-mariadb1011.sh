#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

"$SCRIPT_DIR/run-migration-test.sh" "mariadb1011a" "mariadb1011b" "$PROJECT_ROOT/configs/v1-mariadb1011a-to-mariadb1011b.yaml" "v1 strict-lts: MariaDB 10.11 -> MariaDB 10.11"
