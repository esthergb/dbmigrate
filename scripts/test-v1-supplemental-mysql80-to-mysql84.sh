#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

"$SCRIPT_DIR/run-migration-test.sh" "mysql80a" "mysql84b" "$PROJECT_ROOT/configs/v1-supplemental-mysql80a-to-mysql84b.yaml" "v1 supplemental: MySQL 8.0 -> MySQL 8.4"
