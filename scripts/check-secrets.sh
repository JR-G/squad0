#!/usr/bin/env bash
# Scans staged files for common API key patterns and blocks the commit.
set -euo pipefail

PATTERNS='sk-ant-|xoxb-|xoxp-|ghp_|lin_api_'
EXIT_CODE=0

staged_files=$(git diff --cached --name-only --diff-filter=ACM 2>/dev/null || true)
if [ -z "$staged_files" ]; then
  exit 0
fi

while IFS= read -r file; do
  [ -f "$file" ] || continue
  if grep -nE "$PATTERNS" "$file" > /dev/null 2>&1; then
    echo "ERROR: $file contains what looks like an API key"
    grep -nE "$PATTERNS" "$file" | head -5
    EXIT_CODE=1
  fi
done <<< "$staged_files"

exit $EXIT_CODE
