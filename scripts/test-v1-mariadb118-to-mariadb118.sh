#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

"$SCRIPT_DIR/run-migration-test.sh" "mariadb118a" "mariadb118b" "$PROJECT_ROOT/configs/v1-mariadb118a-to-mariadb118b.yaml" "v1 strict-lts: MariaDB 11.8 -> MariaDB 11.8"
