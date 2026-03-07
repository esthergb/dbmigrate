#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

scenarios=(
  "$SCRIPT_DIR/test-v1-mysql84-to-mysql84.sh"
  "$SCRIPT_DIR/test-v1-mariadb1011-to-mariadb1011.sh"
  "$SCRIPT_DIR/test-v1-mariadb114-to-mariadb114.sh"
  "$SCRIPT_DIR/test-v1-mariadb118-to-mariadb118.sh"
  "$SCRIPT_DIR/test-v1-mysql84-to-mariadb114.sh"
  "$SCRIPT_DIR/test-v1-mariadb114-to-mysql84.sh"
)

for scenario in "${scenarios[@]}"; do
  echo "Running $scenario"
  bash "$scenario"
done
