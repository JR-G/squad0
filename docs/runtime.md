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

Two implementations exist:

| Runtime | Persistence | Use Case |
|---------|------------|----------|
| `ClaudeProcessRuntime` | None | Fresh `claude -p` process per interaction (default) |
| `CodexRuntime` | None | Codex CLI, fresh process per interaction |

Both spawn a fresh subprocess per `Send`. A previous `ClaudePersistentRuntime` that kept a tmux session alive and exchanged messages via a filesystem inbox was removed — it was never the configured default and had become dead code with broken spawn flags.

## Session Bridge

Each agent gets a `SessionBridge` that wraps the active runtime and handles transparent fallback:

```
Orchestrator → bridge.Chat(prompt) → active runtime
                                       ↓ rate limited?
                                   fallback runtime
```

- On rate limit, the bridge swaps to the fallback transparently
- The swap is bidirectional — if Codex is primary and hits limits, Claude is the fallback
- `Execute()` always uses fresh processes (worktree isolation needed for implementation)

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
