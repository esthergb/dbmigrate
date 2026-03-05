#!/usr/bin/env bash
set -euo pipefail

if ! command -v gh >/dev/null 2>&1; then
  echo "error: gh CLI is required" >&2
  exit 1
fi

branch="${1:-$(git branch --show-current)}"
if [[ -z "$branch" ]]; then
  echo "error: unable to determine branch" >&2
  exit 1
fi

echo "Dispatching workflow 'ci' for branch: $branch"
run_url="$(gh workflow run ci.yml --ref "$branch")"
echo "$run_url"

run_id="$(printf '%s' "$run_url" | sed -E 's#.*/runs/([0-9]+).*#\1#')"
if [[ ! "$run_id" =~ ^[0-9]+$ ]]; then
  echo "error: failed to parse run id from workflow dispatch output" >&2
  exit 1
fi

echo "Watching run: $run_id"
gh run watch "$run_id" --exit-status
