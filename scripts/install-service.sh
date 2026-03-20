#!/usr/bin/env bash
# Installs Squad0 as a launchd service that starts on boot and auto-restarts.
set -euo pipefail

INSTALL_DIR="/opt/squad0"
PLIST_SRC="scripts/com.squad0.agent.plist"
PLIST_DST="$HOME/Library/LaunchAgents/com.squad0.agent.plist"
BINARY_SRC="bin/squad0"

echo "=== Installing Squad0 Service ==="

# Build
echo "Building..."
go build -tags sqlite_fts5 -o bin/squad0 ./cmd/squad0
go build -tags sqlite_fts5 -o bin/squad0-memory-mcp ./cmd/squad0-memory-mcp

# Create install directory
if [ ! -d "$INSTALL_DIR" ]; then
  echo "Creating $INSTALL_DIR (requires sudo)..."
  sudo mkdir -p "$INSTALL_DIR"
  sudo chown "$(whoami)" "$INSTALL_DIR"
fi

# Copy files
echo "Copying files..."
cp bin/squad0 "$INSTALL_DIR/squad0"
cp bin/squad0-memory-mcp "$INSTALL_DIR/squad0-memory-mcp"
cp -r agents "$INSTALL_DIR/agents"
cp -r config "$INSTALL_DIR/config"
mkdir -p "$INSTALL_DIR/data/agents" "$INSTALL_DIR/data/logs" "$INSTALL_DIR/data/sessions"

# Unload existing service if running
if launchctl list | grep -q com.squad0.agent; then
  echo "Stopping existing service..."
  launchctl unload "$PLIST_DST" 2>/dev/null || true
fi

# Install plist
echo "Installing launchd plist..."
cp "$PLIST_SRC" "$PLIST_DST"

# Update plist with correct home directory
sed -i '' "s|/Users/james|$HOME|g" "$PLIST_DST"

# Load service
echo "Starting service..."
launchctl load "$PLIST_DST"

echo ""
echo "=== Squad0 Service Installed ==="
echo ""
echo "The orchestrator is now running and will start on boot."
echo ""
echo "Commands:"
echo "  launchctl list | grep squad0     Check if running"
echo "  launchctl unload $PLIST_DST      Stop service"
echo "  launchctl load $PLIST_DST        Start service"
echo "  tail -f $INSTALL_DIR/data/logs/launchd-stdout.log    View logs"
echo ""
echo "Slack commands:"
echo "  stop     Pause all agents"
echo "  start    Resume all agents"
echo "  status   Show agent states"
