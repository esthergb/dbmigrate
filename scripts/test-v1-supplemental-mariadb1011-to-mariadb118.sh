#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

"$SCRIPT_DIR/run-migration-test.sh" "mariadb1011a" "mariadb118b" "$PROJECT_ROOT/configs/v1-supplemental-mariadb1011a-to-mariadb118b.yaml" "v1 supplemental: MariaDB 10.11 -> MariaDB 11.8"
