#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

scenarios=(
  "$SCRIPT_DIR/test-v1-supplemental-mariadb1011-to-mariadb114.sh"
  "$SCRIPT_DIR/test-v1-supplemental-mariadb1011-to-mariadb118.sh"
  "$SCRIPT_DIR/test-v1-supplemental-mariadb114-to-mariadb118.sh"
  "$SCRIPT_DIR/test-v1-supplemental-mysql80-to-mysql84.sh"
)

for scenario in "${scenarios[@]}"; do
  echo "Running $scenario"
  bash "$scenario"
done
