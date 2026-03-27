<p align="center">
  <img src="assets/logo.svg" alt="squad0 logo" width="400" />
</p>

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
task start
```

The install script handles Go tools, git hooks, Ollama, builds, and data directories — skipping anything already installed. After initial setup, use `task start` to build and run.

## Documentation

Full documentation lives in the [`docs/`](docs/) directory:

- **[Architecture](docs/architecture.md)** — Event bus, pipeline stages, session lifecycle, orchestrator coordination
- **[Agents](docs/agents.md)** — Seven roles with models, personality voice system, identity, memory per agent
- **[Communication](docs/communication.md)** — Slack channels, conversation engine, witness pattern, idle duties, narration
- **[Configuration](docs/configuration.md)** — TOML config, Keychain secrets, GitHub App setup
- **[Development](docs/development.md)** — Code standards, testing, git hooks, contributing
- **[Memory](docs/memory.md)** — Beliefs with decay, retrieval strengthening, cross-pollination, concerns, seance, handoffs, findings

## Licence

MIT
