# Configuration

## Config File

`config/squad0.toml`:

```toml
[project]
name = "squad0"
repo = "github.com/JR-G/squad0"
target_repo = ""  # The repo agents work on

[linear]
team_id = ""
project_id = ""

[slack]
bot_token_env = "SLACK_BOT_TOKEN"
app_token_env = "SLACK_APP_TOKEN"
channels = ["feed", "engineering", "reviews", "triage", "standup", "commands"]

[github]
owner = "JR-G"

[embeddings]
provider = "ollama"
model = "nomic-embed-text"
ollama_url = "http://localhost:11434"

[agents]
max_parallel = 3
cooldown_seconds = 300
ticket_batch_size = 3
personality_regen_every = 20

[agents.models]
pm = "claude-haiku-4-5-20251001"
tech_lead = "claude-opus-4-6"
engineer = "claude-sonnet-4-6"
reviewer = "claude-opus-4-6"
designer = "claude-sonnet-4-6"

[quality]
lint = "golangci-lint run"
test = "go test -race ./..."
coverage_threshold = 95

[rituals]
standup_cron = "0 9 * * *"
retro_after_tickets = 10
design_discussion_on_epics = true

[worktree]
base_dir = ".worktrees"
auto_cleanup = true
```

## Secrets

Squad0 stores secrets in macOS Keychain under the service name `squad0`. Only two secrets are needed:

| Secret | Purpose |
|--------|---------|
| `SLACK_BOT_TOKEN` | Slack bot posting (chat:write, chat:write.customize) |
| `SLACK_APP_TOKEN` | Slack socket mode for receiving CEO commands |

Claude Code handles its own Anthropic authentication. Linear and GitHub access is via MCP servers and `gh` CLI respectively — no API keys managed by Squad0.

### Managing Secrets

```bash
squad0 secrets set SLACK_BOT_TOKEN    # Prompts for value, stores in Keychain
squad0 secrets set SLACK_APP_TOKEN
squad0 secrets list                    # Shows which secrets are configured
squad0 secrets verify                  # Checks all required secrets are present
```

## Deployment

Squad0 is designed to run on an always-on macOS machine:

1. Install Ollama and pull the embedding model: `ollama pull nomic-embed-text`
2. Install Claude Code CLI
3. Authenticate `gh` CLI: `gh auth login`
4. Configure Linear MCP server (needs `bun` installed for `bunx`)
5. Set up Slack app with bot token and app-level token
6. Store secrets in Keychain
7. Build and run: `./bin/squad0 start`
