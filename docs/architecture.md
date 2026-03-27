# Architecture

Squad0 is a Go binary that orchestrates multiple Claude Code instances working as a coordinated engineering team. Agents communicate via Slack as distinct personas, pull tickets from Linear, implement in isolated git worktrees, review each other's work, and merge PRs.

## System Overview

```
CEO (phone/laptop)
  |  Slack
Mac Mini (always on)
  +-- squad0 binary
        +-- PM (Sonnet 4.6)       — assigns work, runs rituals, manages board
        +-- Tech Lead (Opus 4.6)   — architecture reviews, design decisions
        +-- Engineer 1 (Sonnet 4.6) — full-stack, thorough/defensive, leans backend
        +-- Engineer 2 (Sonnet 4.6) — full-stack, fast/pragmatic, leans frontend
        +-- Engineer 3 (Sonnet 4.6) — full-stack, architectural, leans infra/DX
        +-- Reviewer (Opus 4.6)    — code review, quality gate
        +-- Designer (Sonnet 4.6)  — UI/UX critique, frontend PR reviews
```

## Design Principles

**Persistent identity, ephemeral sessions.** Agents have permanent identities with accumulated knowledge, but each coding session is short-lived. Sessions are cattle; agents are pets. If a session crashes, the agent's knowledge survives in the DB and personality files.

**Disk-first state.** All state lives on disk — SQLite databases, personality markdown files, check-in records. Nothing important exists only in memory. If the process dies and restarts, it resumes pending work from the pipeline.

**Graceful degradation.** The PM can operate solo if other agents are down. Failed agents are quarantined from new work. The system never hard-crashes because one component fails.

## Startup Sequence

The `start` command (`cmd/squad0/cli/start.go`) runs a deterministic startup:

1. Display TUI banner
2. Initialise logger under `data/logs/`
3. Load secrets from macOS Keychain (`SLACK_BOT_TOKEN`, `SLACK_APP_TOKEN`)
4. Open SQLite databases — one per agent under `data/agents/`, plus `data/project.db`
5. Create Ollama embedder (`nomic-embed-text`)
6. Build all 7 agents with model map, personality loader, memory retriever, hybrid searcher
7. Load or create personas from agent memory DBs
8. Connect Slack bot with rate-limited message queue (2s spacing)
9. Run introductions — new agents choose a name and post to `#feed`
10. Run PM briefing (first startup only, gated by `data/.briefing_done`)
11. Create coordination DB (`data/coordination.db`) for check-ins and pipeline
12. Wire up health monitor, scheduler, assigner
13. Create pipeline stores (work items, handoffs)
14. Create event bus and register default handlers
15. Build conversation engine with per-agent fact stores and project fact store
16. Seed conversation history from Slack channel history
17. Configure GitHub App token for Reviewer, PM, and Tech Lead (if credentials exist)
18. Start three concurrent loops: Slack event listener, scheduler, orchestrator

## Event Bus

The `EventBus` (`internal/orchestrator/eventbus.go`) decouples pipeline stages. Handlers run in separate goroutines with panic recovery so one failing handler cannot break others.

### Event Kinds

| Event | Trigger | Default Handler |
|-------|---------|-----------------|
| `pr_approved` | Reviewer approves PR | Engineer merges their own PR |
| `changes_requested` | Reviewer requests fixes | Engineer runs fix-up session |
| `fixup_complete` | Engineer finishes addressing feedback | Reviewer re-reviews |
| `merge_failed` | Merge attempt fails | Engineer runs fix-up session |
| `merge_complete` | PR merged | Informational |
| `session_complete` | Engineer finishes implementation | Informational |
| `session_failed` | Engineer session errors out | Informational |
| `agent_idle` | Agent has no work | Idle duties triggered |
| `review_complete` | Review session finishes | Informational |

When the bus is nil (e.g. in tests), `emitOrFallback` calls the handler directly.

## Pipeline Stages

The `pipeline` package (`internal/pipeline/`) tracks work items through a linear progression:

```
assigned -> working -> pr_opened -> reviewing -> approved -> merged
                                       |
                                       v
                                changes_requested -> working (fix-up) -> reviewing (re-review)
                                                                            |
                                                                      (max 3 cycles -> #triage)
```

Terminal stages: `merged`, `failed`.

Each `WorkItem` records the ticket, engineer, reviewer, PR URL, branch, review cycle count, and timestamps. The `HandoffStore` captures session state (status, summary, branch, git state, blockers) so reassigned tickets carry forward what the previous agent learned.

## Session Lifecycle

### 1. Assignment

The PM queries Linear for unstarted/backlog tickets via a `DirectSession` that runs `curl` against the Linear GraphQL API. Returns JSON assignments mapping tickets to idle engineers.

### 2. Discussion Phase

Before implementation, the engineer posts a plan to `#engineering` via `QuickChat`. The Tech Lead always weighs in with architectural guidance. The conversation engine lets other agents respond. The orchestrator waits for the thread to go quiet (polling `IsQuiet`, max 3 minutes), then the PM breaks any ties with a `Decision:` statement that is stored as a belief.

### 3. Implementation

A `WorkSession` creates a git worktree at `{target_repo}/.worktrees/{role}` on a `feat/{ticket}` branch. The `.mcp.json` is written to the worktree with Linear and memory MCP servers. The agent runs `ExecuteTask` which:

- Loads the base personality from `agents/{role}.md`
- Runs mandatory memory retrieval (hybrid search + graph traversal)
- Injects seance context (prior work on this ticket by other agents, cross-agent beliefs, handoff history)
- Injects discussion context from the planning thread
- Runs the Claude Code session with `--dangerously-skip-permissions`

### 4. Post-Implementation

After the session completes:

- Extract PR URL from the transcript
- Write a handoff record
- Run pre-submit check (git status, uncommitted changes, test suite)
- Store a project episode for cross-agent seance
- Persist findings to the Linear ticket as a comment (if the transcript contains discovery keywords)
- Flush session memory via PM extraction (orchestrator-driven fallback)
- If a PR exists, start the review pipeline

### 5. Review

The Reviewer reads the diff, PR description, and existing comments via `gh` CLI in a `DirectSession`. It posts detailed findings as a PR comment and submits an official GitHub review. The outcome is classified from the transcript.

On approval:
- Force-submit the GitHub approval
- Architecture review runs in the background (2 minute timeout so a stuck Opus session cannot block the pipeline)
- Engineer merges their own PR

On changes requested (up to 3 cycles):
- Engineer runs a fix-up session reading review comments
- Handoff context carries forward
- Reviewer re-reviews focusing on their previous comments
- After 3 cycles, escalate to `#triage`

### 6. Merge

Engineers own their merges. The approved engineer reads remaining comments, rebases if needed, checks CI, and runs `gh pr merge --squash --delete-branch`. The orchestrator verifies the merge via `gh pr view --json state`. On failure, the event bus triggers a fix-up. The PM handles fallback merges when the engineer is unavailable, including approval verification and retry logic.

### 7. Resume on Restart

On startup, `resumePendingWork` scans the pipeline for non-terminal work items and resumes them based on stage: reviewing items get re-reviewed, approved items get merged, changes-requested items get fixed up, stale working items (>30 minutes with no PR) are marked failed and returned to backlog.

## Orchestrator as Coordinator

The orchestrator does not implement business logic itself — it coordinates. Each tick:

1. `breakSilence` — conversation engine starts a topic if channels are quiet
2. `RunWitnessScan` — Tech Lead and PM scan for unanswered questions
3. `RunPMDuties` — check for stale work items, nudge engineers
4. Check for idle agents
5. Engage idle non-engineers (Designer, Tech Lead) with idle duties
6. Filter healthy engineers, check WIP limits
7. Request PM assignments for truly idle engineers
8. Start work sessions in goroutines

## GitHub App Integration

Squad0 supports a GitHub App for PR approvals (`internal/integrations/github/apptoken.go`). The app's private key PEM is stored base64-encoded in Keychain as `GITHUB_APP_PRIVATE_KEY` (Keychain strips newlines from multi-line values, so the PEM must be base64-encoded). At startup, the provider generates a JWT, exchanges it for an installation token, and injects it as `GH_TOKEN` for the Reviewer, PM, and Tech Lead. Tokens are cached and refreshed automatically with a 5 minute buffer.

## Package Structure

```
cmd/
  squad0/              Entry point and CLI commands
  squad0-memory-mcp/   MCP server binary for agent memory access

internal/
  agent/               Agent lifecycle, session management, prompt assembly, MCP config
  config/              TOML config loading, validation, defaults
  coordination/        Check-in SQLite store and file conflict detection
  health/              Agent health tracking (healthy/slow/stuck/failing/idle) and Slack alerts
  integrations/
    github/            GitHub App token provider (JWT + installation token)
    slack/             Bot, personas, commands, listener, links, rate limiter, formatting
  logging/             Structured JSON logging and session capture
  mcp/                 MCP JSON-RPC protocol and memory tool handlers
  memory/              Knowledge graph, facts, beliefs, episodes, hybrid search, evolution, flush
  orchestrator/        Main loop, assigner, scheduler, lifecycle, conversation, review, merge
  pipeline/            Work item stages, handoffs, attribution stats
  secrets/             macOS Keychain secret management
  tui/                 Terminal UI banner, dashboard, styles
  worktree/            Git worktree creation and cleanup
```

## Concurrency Model

- Each agent session runs in its own goroutine with a cancellable context
- The `sync.WaitGroup` on the orchestrator tracks all running sessions
- Session cancellation functions are stored in a mutex-protected map
- SQLite databases use WAL mode with 5 second busy timeout for concurrent access
- The conversation engine uses a mutex per operation
- The Slack rate limiter enforces 2 second minimum spacing between messages
