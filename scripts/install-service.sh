#!/usr/bin/env bash
# Installs Squad0 as a launchd service that starts on boot and auto-restarts.
# Usage: ./scripts/install-service.sh
set -euo pipefail

REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
INSTALL_DIR="/opt/squad0"
PLIST_SRC="$REPO_DIR/scripts/com.squad0.agent.plist"
PLIST_DST="$HOME/Library/LaunchAgents/com.squad0.agent.plist"

echo "=== Installing Squad0 Service ==="

# Preflight checks
for cmd in go claude gh; do
  if ! command -v "$cmd" > /dev/null 2>&1; then
    echo "ERROR: $cmd is not installed."
    exit 1
  fi
done

# Build
echo "Building..."
cd "$REPO_DIR"
go build -tags sqlite_fts5 -o bin/squad0 ./cmd/squad0
go build -tags sqlite_fts5 -o bin/squad0-memory-mcp ./cmd/squad0-memory-mcp

# Create install directory
if [ ! -d "$INSTALL_DIR" ]; then
  echo "Creating $INSTALL_DIR (requires sudo)..."
  sudo mkdir -p "$INSTALL_DIR"
  sudo chown "$(whoami)" "$INSTALL_DIR"
fi

# Unload existing service if running
if launchctl list 2>/dev/null | grep -q com.squad0.agent; then
  echo "Stopping existing service..."
  launchctl unload "$PLIST_DST" 2>/dev/null || true
  sleep 1
fi

# Copy files
echo "Copying files to $INSTALL_DIR..."
cp bin/squad0 "$INSTALL_DIR/squad0"
cp bin/squad0-memory-mcp "$INSTALL_DIR/squad0-memory-mcp"
rsync -a --delete agents/ "$INSTALL_DIR/agents/"
rsync -a --delete config/ "$INSTALL_DIR/config/"
mkdir -p "$INSTALL_DIR/data/agents" "$INSTALL_DIR/data/logs" "$INSTALL_DIR/data/sessions"

# Install plist
echo "Installing launchd plist..."
mkdir -p "$HOME/Library/LaunchAgents"
sed "s|/Users/james|$HOME|g" "$PLIST_SRC" > "$PLIST_DST"

# Load service
echo "Starting service..."
launchctl load "$PLIST_DST"

echo ""
echo "=== Squad0 Service Installed ==="
echo ""
echo "The orchestrator is now running and will start on boot."
echo ""
echo "Manage:"
echo "  launchctl list | grep squad0            Check status"
echo "  launchctl unload $PLIST_DST   Stop"
echo "  launchctl load $PLIST_DST     Start"
echo "  tail -f $INSTALL_DIR/data/logs/launchd-stdout.log"
echo ""
echo "Or use Slack:"
echo "  stop / start / status in #commands"
