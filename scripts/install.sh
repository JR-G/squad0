#!/usr/bin/env bash
# Squad0 setup script — installs dependencies, configures hooks, and builds.
# Skips anything already installed.
set -euo pipefail

GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

log() { echo -e "${GREEN}✓${NC} $1"; }
skip() { echo -e "${YELLOW}→${NC} $1 (already installed)"; }

# Go tools
install_go_tool() {
  local name=$1
  local pkg=$2

  if command -v "$name" > /dev/null 2>&1; then
    skip "$name"
  else
    echo "  Installing $name..."
    go install "$pkg"
    log "$name installed"
  fi
}

echo "=== Squad0 Setup ==="
echo ""

# Check Go
if ! command -v go > /dev/null 2>&1; then
  echo "ERROR: Go is not installed. Install Go 1.22+ first."
  exit 1
fi
log "Go $(go version | awk '{print $3}')"

# Ensure ~/go/bin is in PATH
if [[ ":$PATH:" != *":$HOME/go/bin:"* ]]; then
  export PATH="$HOME/go/bin:$PATH"
  if ! grep -q 'go/bin' ~/.zshrc 2>/dev/null; then
    echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.zshrc
    log "Added ~/go/bin to PATH in .zshrc"
  fi
fi

# Install Go tools
echo ""
echo "--- Go Tools ---"
install_go_tool "gofumpt" "mvdan.cc/gofumpt@latest"
install_go_tool "golangci-lint" "github.com/golangci/golangci-lint/cmd/golangci-lint@latest"
install_go_tool "lefthook" "github.com/evilmartians/lefthook@latest"

# Check bun (needed for Linear MCP server)
echo ""
echo "--- Runtime Dependencies ---"
if command -v bun > /dev/null 2>&1; then
  skip "bun"
else
  echo "  WARNING: bun is not installed. Needed for Linear MCP server."
  echo "  Install: curl -fsSL https://bun.sh/install | bash"
fi

# Check Ollama (needed for embeddings)
if command -v ollama > /dev/null 2>&1; then
  skip "ollama"
  if ollama list 2>/dev/null | grep -q "nomic-embed-text"; then
    skip "nomic-embed-text model"
  else
    echo "  Pulling nomic-embed-text model..."
    ollama pull nomic-embed-text
    log "nomic-embed-text model pulled"
  fi
else
  echo "  WARNING: ollama is not installed. Needed for embeddings."
  echo "  Install: https://ollama.ai"
fi

# Check gh CLI
if command -v gh > /dev/null 2>&1; then
  skip "gh CLI"
else
  echo "  WARNING: gh CLI is not installed. Needed for PR management."
  echo "  Install: brew install gh"
fi

# Git hooks
echo ""
echo "--- Git Hooks ---"
lefthook install
log "Lefthook hooks installed"

# Ensure hooks can find Go tools
for hook in .git/hooks/pre-commit .git/hooks/pre-push; do
  if [ -f "$hook" ] && ! grep -q 'go/bin' "$hook"; then
    sed -i '' '2i\
export PATH="$HOME/go/bin:$PATH"\
' "$hook"
    log "Added PATH to $hook"
  fi
done

# Build
echo ""
echo "--- Build ---"
go build -tags sqlite_fts5 -o bin/squad0 ./cmd/squad0
log "bin/squad0 built"

go build -tags sqlite_fts5 -o bin/squad0-memory-mcp ./cmd/squad0-memory-mcp
log "bin/squad0-memory-mcp built"

# Create data directories
mkdir -p data/agents data/logs data/sessions
log "Data directories created"

# Config
if [ ! -f config/squad0.toml ]; then
  echo "  WARNING: config/squad0.toml not found. Copy and edit the default:"
  echo "  cp config/squad0.toml.example config/squad0.toml"
fi

echo ""
echo "=== Setup Complete ==="
echo ""
echo "Next steps:"
echo "  1. Configure secrets:  ./bin/squad0 secrets set SLACK_BOT_TOKEN"
echo "  2. Configure secrets:  ./bin/squad0 secrets set SLACK_APP_TOKEN"
echo "  3. Edit config:        config/squad0.toml"
echo "  4. Start:              ./bin/squad0 start"
