#!/usr/bin/env bash
# Verifies test coverage meets the minimum threshold.
# On failure, shows the worst offenders so you know exactly what to fix.
set -euo pipefail

MIN_COVERAGE=${1:-95}

go test -tags sqlite_fts5 -timeout 120s -coverprofile=coverage.out ./... 2>&1

TOTAL=$(go tool cover -func=coverage.out | grep total | awk '{print $NF}' | tr -d '%')

if [ -z "$TOTAL" ]; then
  echo "ERROR: could not determine coverage"
  exit 1
fi

TOTAL_INT=$(echo "$TOTAL" | awk '{printf "%d", $1 + 0.5}')

if [ "$TOTAL_INT" -lt "$MIN_COVERAGE" ]; then
  echo ""
  echo "ERROR: coverage ${TOTAL}% is below minimum ${MIN_COVERAGE}%"
  echo ""
  echo "=== Worst offenders (lowest coverage functions) ==="
  echo ""
  go tool cover -func=coverage.out | awk '$3+0 > 0 && $3+0 < 80' | sort -t'%' -k3 -n | head -10
  echo ""
  echo "=== Packages below 95% ==="
  echo ""
  go tool cover -func=coverage.out | grep "^total" > /dev/null
  grep "coverage:" coverage.out 2>/dev/null || true
  # Show per-package coverage from the test output
  echo ""
  echo "Fix the worst offenders above to raise total coverage."
  rm -f coverage.out
  exit 1
fi

echo "Coverage: ${TOTAL}% (minimum: ${MIN_COVERAGE}%)"
rm -f coverage.out
