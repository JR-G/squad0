#!/usr/bin/env bash
# Rebuild and restart squad0.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

echo "Building..."
go build -tags sqlite_fts5 -o bin/squad0 ./cmd/squad0

pkill squad0 2>/dev/null || true
sleep 1

echo "Starting..."
exec ./bin/squad0 start
