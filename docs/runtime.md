# Runtime Architecture

Squad0 uses a runtime-agnostic abstraction for agent session execution. Both Claude Code and Codex are first-class peers — configurable per agent, switchable at any time. Neither is "the real one" and "the backup."

## Runtime Interface

Every runtime implements the same interface:

```go
type Runtime interface {
    Start(ctx context.Context, cfg StartConfig) error
    Send(ctx context.Context, prompt string) (string, error)
    IsAlive() bool
    Stop() error
    Name() string
    SupportsHooks() bool
}
```

Three implementations exist:

| Runtime | Persistence | Hooks | Use Case |
|---------|------------|-------|----------|
| `ClaudePersistentRuntime` | tmux session | Yes | Chat — personality loaded once, context cached |
| `ClaudeProcessRuntime` | None | No | Fresh process per interaction (current default) |
| `CodexRuntime` | None | No | Codex CLI, fresh process per interaction |

## Session Bridge

Each agent gets a `SessionBridge` that wraps the active runtime and handles transparent fallback:

```
Orchestrator → bridge.Chat(prompt) → active runtime
                                       ↓ rate limited?
                                   fallback runtime
```

- `Chat()` routes through the persistent session when available, fresh process otherwise
- On rate limit, the bridge swaps to the fallback transparently
- The swap is bidirectional — if Codex is primary and hits limits, Claude is the fallback
- `Execute()` always uses fresh processes (worktree isolation needed for implementation)

## Persistent Sessions (tmux + hooks)

For runtimes that support hooks (Claude Code), sessions persist in tmux:

**Startup:**
1. `ClaudePersistentRuntime.Start` creates a tmux session: `squad0-{role}`
2. Claude Code launches with `--dangerously-skip-permissions`
3. The `SessionStart` hook runs `squad0 prime --role {role}` — prints personality to stdout
4. Claude Code captures this as system context. Personality loaded once.

**Message delivery:**
1. The bridge calls `Send(prompt)` which writes a JSON file to `data/inbox/{role}/`
2. At the next turn boundary, Claude Code's `UserPromptSubmit` hook fires
3. The hook runs `squad0 inbox drain --role {role}` — reads the inbox, prints as `<system-reminder>` blocks
4. Claude processes the message and writes a response to `data/outbox/{role}/`
5. The bridge polls the outbox and returns the response

**Self-healing:**
If the tmux session dies (crash, OOM, manual kill), `Send` detects this via `IsAlive()`, calls `Start` to restart, and retries once. The personality is re-injected by the SessionStart hook.

## Inbox/Outbox Queue

Filesystem-based message queue for persistent session communication:

```
data/
  inbox/
    engineer-1/
      1712345678-12345.json        ← prompt waiting for delivery
      1712345678-12345.json.claimed ← being processed
    engineer-2/
      ...
  outbox/
    engineer-1/
      1712345678-12345-response.json ← agent's response
    ...
```

- **Atomic writes**: write to `.tmp`, rename to `.json`
- **Atomic claims**: rename `.json` to `.claimed` during drain
- **Per-agent directories**: no cross-agent interference
- **Hook output**: `FormatDrained()` wraps messages in `<system-reminder>` tags

## Configuration

```toml
[agents.runtime]
default = "claude"      # Primary runtime for all agents
fallback = "codex"      # Fallback on rate limits

[agents.runtime.overrides]
engineer-1 = "codex"    # Per-role override
```

Valid runtime names: `"claude"`, `"codex"`. Both are equal peers. The `default` is what agents start with; `fallback` is what they swap to on rate limits. Overrides apply per role.

## Adding a New Runtime

Implement the `Runtime` interface in `internal/runtime/`:

1. Create `your_runtime.go` with the six interface methods
2. Add the name to `validRuntimes` in `internal/config/config.go`
3. Wire creation in `cmd/squad0/cli/startup_runtime.go`
4. Add tests in `your_runtime_test.go`

The bridge and config system handle the rest — your runtime becomes available as a first-class peer.
