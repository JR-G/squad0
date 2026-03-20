#!/usr/bin/env bash
# Checks that no .go file exceeds 500 lines.
set -euo pipefail

MAX_LINES=500
EXIT_CODE=0

while IFS= read -r -d '' file; do
  lines=$(wc -l < "$file")
  if [ "$lines" -gt "$MAX_LINES" ]; then
    echo "ERROR: $file has $lines lines (max $MAX_LINES)"
    EXIT_CODE=1
  fi
done < <(find . -name '*.go' -not -path './vendor/*' -print0)

exit $EXIT_CODE
