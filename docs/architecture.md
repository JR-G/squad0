# Architecture

## Overview

Squad0 is a Go binary that orchestrates multiple Claude Code instances working as a coordinated, self-organising engineering team. Each agent is a short-lived Claude Code session with a persistent identity backed by a personal knowledge graph.

```
CEO (phone/laptop)
  ↕ Slack
Host machine (always on)
  └── squad0 binary
        ├── PM (Haiku 4.5) — assigns work, runs rituals, manages board
        ├── Tech Lead (Opus 4.6) — reviews PRs, architecture decisions
        ├── Engineer 1 (Sonnet 4.6) — full-stack, thorough/defensive
        ├── Engineer 2 (Sonnet 4.6) — full-stack, fast/pragmatic
        ├── Engineer 3 (Sonnet 4.6) — full-stack, architectural
        ├── Reviewer (Opus 4.6) — code review, quality gate
        └── Designer (Sonnet 4.6) — UI/UX critique
```

## Design Principles

**Persistent identity, ephemeral sessions.** Agents have permanent identities with accumulated knowledge, but each coding session is short-lived. Sessions are cattle; agents are pets.

**Disk-first state.** All state lives on disk — SQLite databases, personality files, check-in records. If the process dies and restarts, it picks up where it left off.

**Graceful degradation.** The PM can operate solo if other agents are down. Rate-limited agents pause and resume. Failed agents are quarantined and their work reassigned.

**MCP-first integration.** Agents use Linear MCP server and `gh` CLI directly. The Go orchestrator is a pure process manager — it never calls Linear or GitHub APIs.

## Core Loop

1. Orchestrator checks which engineers are idle (coordination DB)
2. Orchestrator spawns PM session: "given these idle engineers and the board, assign work"
3. PM reads Linear via MCP, responds with structured assignment JSON
4. Orchestrator parses assignments, spawns engineer sessions in isolated worktrees
5. Engineers work, using MCP memory tools to recall and store knowledge
6. Orchestrator captures session output, runs fallback memory extraction
7. Health monitor tracks progress, alerts on stuck/failing agents
8. Loop continues

## Package Structure

```
cmd/
  squad0/              Entry point for the orchestrator binary
  squad0-memory-mcp/   MCP server for agent memory access

internal/
  agent/               Agent lifecycle, spawning, prompt assembly
  config/              TOML config loading and validation
  coordination/        Check-in DB and file conflict detection
  health/              Agent health tracking and Slack alerts
  integrations/slack/  Bot, personas, commands, rate limiting
  logging/             Structured JSON logging and session capture
  mcp/                 MCP protocol and memory tool handlers
  memory/              Knowledge graph, hybrid search, evolution
  orchestrator/        Main loop, assigner, scheduler, lifecycle
  secrets/             macOS Keychain secret management
  worktree/            Git worktree creation and cleanup

agents/                Base personality files for each role
config/                Default configuration
docs/                  This documentation
scripts/               Git hook scripts
```

## Data Flow

```
Ticket (Linear)
  → PM assigns via MCP
    → Engineer spawned in worktree
      → MCP memory: recall relevant knowledge
      → Implementation with atomic commits
      → MCP memory: store learnings
      → PR created via gh CLI
    → Reviewer reviews PR
    → Tech Lead reviews architecture
  → Orchestrator captures output
  → Memory flush (fallback extraction)
  → Health metrics updated
  → Episode stored with embedding
```
