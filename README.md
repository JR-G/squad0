# Squad0

An autonomous software engineering team. Agents with evolving personalities pull tickets from Linear, discuss approaches in Slack, implement in isolated git worktrees, review each other's work, and open PRs.

## Quick Start

```bash
# Clone and install everything
git clone https://github.com/JR-G/squad0.git
cd squad0
./scripts/install.sh

# Configure secrets
./bin/squad0 secrets set SLACK_BOT_TOKEN
./bin/squad0 secrets set SLACK_APP_TOKEN

# Run
./bin/squad0 start
```

The install script handles Go tools, git hooks, Ollama, builds, and data directories — skipping anything already installed.

## Documentation

Full documentation lives in the [`docs/`](docs/) directory:

- **[Architecture](docs/architecture.md)** — System design, agent roles, and how everything fits together
- **[Agents](docs/agents.md)** — Agent roles, personalities, identity system, and how they evolve
- **[Memory System](docs/memory.md)** — Knowledge graph, hybrid search, MCP tools, and cognitive psychology principles
- **[Communication](docs/communication.md)** — Slack channels, personas, CEO commands, and agent discussions
- **[Configuration](docs/configuration.md)** — Config file, secrets, and deployment setup
- **[Development](docs/development.md)** — Code standards, testing, git hooks, and contributing

## Licence

MIT
