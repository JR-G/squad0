# Configuration

## Config File

`config/squad0.toml` — all settings with their actual defaults from the codebase:

### [project]

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `name` | string | `"squad0"` | Project name (must not be empty) |
| `repo` | string | `""` | Squad0's own repository URL |
| `target_repo` | string | `""` | The repository agents work on (e.g. `"github.com/JR-G/makebook"`) |

The `target_repo` is resolved to `~/repos/{basename}` at startup. If empty, work sessions have no target directory.

### [linear]

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `team_id` | string | `""` | Linear team UUID for ticket queries |
| `project_id` | string | `""` | Linear project ID (optional) |
| `workspace` | string | `""` | Linear workspace slug (e.g. `"jamesrg"`) — used for ticket links |

If `team_id` is empty, the orchestrator runs in chat-only mode — agents converse but do not pull tickets.

### [slack]

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `bot_token_env` | string | `"SLACK_BOT_TOKEN"` | Env var name for the bot token (not used directly — secrets come from Keychain) |
| `app_token_env` | string | `"SLACK_APP_TOKEN"` | Env var name for the app-level token |

#### [slack.channels]

A map of logical channel names to Slack channel IDs:

```toml
[slack.channels]
commands = "C0ANVV4HT08"
engineering = "C0AMY8QM9GV"
feed = "C0AML7VJLMD"
reviews = "C0ANVV5MY9W"
standup = "C0AMZKGP8LE"
triage = "C0AN1M555FC"
chitchat = "C0ANYT4845R"
```

The `commands` channel is mandatory — validation fails without it. All other channels are optional but expected for full functionality.

### [github]

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `owner` | string | `""` | GitHub organisation or user (e.g. `"JR-G"`) |

### [embeddings]

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `provider` | string | `"ollama"` | Must be `"ollama"` (the only supported provider) |
| `model` | string | `"nomic-embed-text"` | Ollama model name for embeddings |
| `ollama_url` | string | `"http://localhost:11434"` | Ollama API endpoint |

Embeddings run locally via Ollama — free, offline, no API costs.

### [agents]

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `max_parallel` | int | `3` | Maximum concurrent engineer sessions (1-10) |
| `cooldown_seconds` | int | `300` | Poll interval between ticks (also used as cooldown) |
| `ticket_batch_size` | int | `3` | Maximum tickets per PM assignment request |
| `personality_regen_every` | int | `20` | Sessions between personality regeneration |

#### [agents.models]

| Field | Default | Description |
|-------|---------|-------------|
| `pm` | `"claude-sonnet-4-6"` | PM model |
| `tech_lead` | `"claude-opus-4-6"` | Tech Lead model |
| `engineer` | `"claude-sonnet-4-6"` | Shared model for all 3 engineers |
| `reviewer` | `"claude-opus-4-6"` | Reviewer model |
| `designer` | `"claude-sonnet-4-6"` | Designer model |

### [quality]

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `lint` | string | `"golangci-lint run"` | Lint command |
| `test` | string | `"go test -race ./..."` | Test command |
| `coverage_threshold` | int | `95` | Minimum test coverage percentage (0-100) |

### [rituals]

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `standup_cron` | string | `"0 9 * * *"` | Standup schedule (only the hour field is used) |
| `retro_after_tickets` | int | `10` | Trigger retro after this many completed tickets |
| `design_discussion_on_epics` | bool | `true` | Auto-trigger design discussions for epics |

### [worktree]

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `base_dir` | string | `".worktrees"` | Directory under the target repo for agent worktrees |
| `auto_cleanup` | bool | `true` | Automatically remove worktrees after session ends |

## Secrets

Squad0 stores secrets in macOS Keychain under the service name `squad0`. Secrets are read at startup and held in memory for the process lifetime — never logged, never serialised.

### Required Secrets

| Secret | Purpose |
|--------|---------|
| `SLACK_BOT_TOKEN` | Slack bot posting (`chat:write`, `chat:write.customize`) |
| `SLACK_APP_TOKEN` | Slack socket mode for receiving CEO commands |

### Optional Secrets (GitHub App)

| Secret | Purpose |
|--------|---------|
| `GITHUB_APP_ID` | GitHub App ID |
| `GITHUB_APP_INSTALLATION_ID` | Installation ID for the target repository |
| `GITHUB_APP_PRIVATE_KEY` | Private key PEM, base64-encoded |

Claude Code handles its own Anthropic authentication. Linear access is via MCP server. GitHub access for engineers is via `gh` CLI authentication.

### Managing Secrets

```bash
squad0 secrets set SLACK_BOT_TOKEN    # Prompts for value, stores in Keychain
squad0 secrets set SLACK_APP_TOKEN
squad0 secrets list                    # Shows which secrets are configured (names only)
squad0 secrets verify                  # Checks all required secrets are present
```

### GitHub App Setup

The GitHub App provides a separate identity for PR approvals (so the Reviewer's approval counts as a different user from the repository owner).

1. Create a GitHub App with `pull_requests: write` and `contents: write` permissions
2. Install it on the target repository
3. Download the private key `.pem` file
4. Base64-encode the PEM (Keychain strips newlines from multi-line values):

```bash
base64 -i your-app.pem | pbcopy
```

5. Store all three values:

```bash
security add-generic-password -s squad0 -a GITHUB_APP_ID -w "123456" -U
security add-generic-password -s squad0 -a GITHUB_APP_INSTALLATION_ID -w "789012" -U
security add-generic-password -s squad0 -a GITHUB_APP_PRIVATE_KEY -w "$(pbpaste)" -U
```

At startup, Squad0 decodes the base64 PEM, generates a JWT, exchanges it for an installation token, and injects it as `GH_TOKEN` for the Reviewer, PM, and Tech Lead agents. Tokens are cached and refreshed automatically.

If the GitHub App is not configured, reviews use the repository owner's token (from `gh auth login`).

## Prerequisites

- **Go 1.22+** with modules
- **Ollama** with `nomic-embed-text` model: `ollama pull nomic-embed-text`
- **Claude Code CLI** installed and authenticated
- **`gh` CLI** authenticated: `gh auth login`
- **`bun`** installed (for Linear MCP server): `curl -fsSL https://bun.sh/install | bash`
- **Slack app** with bot token and app-level token (socket mode enabled)

## Validation

```bash
squad0 config validate    # Validates config/squad0.toml
```

This checks all required fields, value ranges, model assignments, and channel configuration.
