#!/usr/bin/env bash
# Verifies test coverage meets the minimum threshold.
set -euo pipefail

MIN_COVERAGE=${1:-95}

go test -race -tags sqlite_fts5 -timeout 120s -coverprofile=coverage.out ./... 2>&1

TOTAL=$(go tool cover -func=coverage.out | grep total | awk '{print $NF}' | tr -d '%')

if [ -z "$TOTAL" ]; then
  echo "ERROR: could not determine coverage"
  exit 1
fi

TOTAL_INT=$(echo "$TOTAL" | awk '{printf "%d", $1 + 0.5}')

if [ "$TOTAL_INT" -lt "$MIN_COVERAGE" ]; then
  echo "ERROR: coverage ${TOTAL}% is below minimum ${MIN_COVERAGE}%"
  exit 1
fi

echo "Coverage: ${TOTAL}% (minimum: ${MIN_COVERAGE}%)"
rm -f coverage.out
