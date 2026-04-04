# Agents

## Roles and Models

| Role | Model | Purpose |
|------|-------|---------|
| PM | Sonnet 4.6 | Assigns work, runs rituals, manages board, breaks discussion ties |
| Tech Lead | Opus 4.6 | Architecture reviews, design decisions, weighs in on every discussion |
| Engineer 1 | Sonnet 4.6 | Full-stack, thorough and defensive, leans backend |
| Engineer 2 | Sonnet 4.6 | Full-stack, fast and pragmatic, leans frontend |
| Engineer 3 | Sonnet 4.6 | Full-stack, architectural, leans infra/DX |
| Reviewer | Opus 4.6 | Code review via `gh` CLI, quality gate, catches subtle bugs |
| Designer | Sonnet 4.6 | UI/UX critique, reviews frontend PRs, user-centred thinking |

Models are configurable in `config/squad0.toml` under `[agents.models]`. Opus is used where deep reasoning matters (architecture and review); Sonnet where speed and cost efficiency are more important.

## Identity System

Agents choose their own names on first startup. The name is stored in their personal knowledge graph as a high-confidence identity fact and used for all Slack messages.

### How Name Selection Works

1. On first run, each agent without a stored name gets the introduction prompt
2. The agent picks a name and introduces themselves
3. The orchestrator extracts the name using pattern matching ("My name is X", "I'm X", etc.)
4. `PersonaStore.SaveChosenName()` writes it as a fact with confidence 1.0 and 100 confirmations (effectively permanent)
5. All future sessions load the name from the agent's memory DB
6. Slack messages display `{Name} — {Role Title}` with an auto-generated identicon avatar from a hash of the name

There is no CEO veto — agents are free to choose any name.

## Personality Voice System

Each agent has a base personality file (`agents/{role}.md`) containing:

- **Role description** — what they do, how they think
- **Voice section** — how they communicate (tone, sentence structure, verbal habits)
- **Communication Style section** — how they interact with the team
- **How You Work** — working principles and approach
- **Memory instructions** — how to use MCP tools

The voice section is particularly important. During conversations, the `ConversationEngine` extracts only the `## Voice` and `## Communication Style` sections from the personality file via `LoadVoice()` and injects them into chat prompts. This keeps conversations lightweight while maintaining distinct voices.

### Voice Examples

**PM:** "Crisp and decisive. No corporate speak, ever. You say 'let's do X' not 'I think we should consider the possibility of X'."

**Engineer 1:** "You speak like someone writing a postmortem before the incident happens. Dry, understated, slightly wary."

**Tech Lead:** "Considered and deliberate. You reason out loud: 'if we go with approach A, the consequence is X, which means Y'."

## Personality Evolution

Base personality files set the starting point. The real personality develops through experience.

### Belief Accumulation

Every session, the agent stores learnings as beliefs and facts via MCP memory tools. Beliefs have confidence scores that increase with confirmation and decrease with contradictions. The `TopBeliefs` query applies temporal decay so stale beliefs drop naturally.

### Personality Regeneration

Every N sessions (configurable via `personality_regen_every`, default 20), a regeneration step runs via `GeneratePersonalitySummary()`. It reads the agent's top beliefs and appends a "Learned Beliefs" section to the personality, categorising them by strength (strong/moderate/weak).

### Divergence

Two agents with identical starting personalities diverge naturally because they work on different tickets, form different beliefs, and confirm different patterns. After enough sessions, each agent has genuinely unique expertise.

## Memory Per Agent

Each agent has a personal SQLite database at `data/agents/{role}.db` containing:

- **Entities**: modules, files, patterns, tools, concepts the agent knows about
- **Facts**: specific learnings tied to entities, with confidence and access tracking
- **Beliefs**: causal beliefs that evolve with evidence and decay over time
- **Episodes**: session logs with embeddings for semantic search
- **FTS5 indexes**: full-text search across facts, beliefs, and episodes

At session start, the `Retriever` runs mandatory hybrid search (vector similarity + BM25 keyword) and graph traversal to assemble relevant context. This is not optional — it is built into `assemblePrompt()`.

The MCP memory server (`cmd/squad0-memory-mcp/`) exposes five tools: `recall`, `remember_fact`, `store_belief`, `note_entity`, `recall_entity`. Agents use these during sessions to manage their own memory in real time.

## Session Types

### ExecuteTask

Full agent session with personality loading, memory retrieval, and episode storage. Used for implementation and introductions.

### QuickChat

Lightweight session using only the voice section of the personality. Uses the `claude-haiku-4-5-20251001` model for speed. Used for Slack conversations, discussion plans, and acknowledgements.

### DirectSession

Clean session with the agent's own model, no personality wrapping, no memory retrieval. Used for structured tasks: querying Linear, running `gh` commands for review/merge, memory extraction.

## Ticket Creation

Agents can create Linear tickets during sessions when they discover:

- **Bugs**: broken functionality outside their current scope
- **Tech debt**: code that works but violates standards
- **Scope overflow**: ticket is bigger than expected — create child tickets, do not expand scope

All agent-created tickets go to backlog for CEO triage via `#triage`.

## Emergent Specialisation

Agents develop specialisations from actual outcomes, not from their personality files. The `SpecialisationStore` tracks success/failure rates per ticket category. Over time, the PM routes work to agents who've historically succeeded on similar tickets.

This means Engineer-1's personality says "leans backend" but the data might show they're actually best at auth tickets specifically. The specialisation is earned, not assigned.

## Inter-Agent Opinions

Agents form beliefs about each other's work quality based on review outcomes. A reviewer who consistently approves an engineer's PRs with zero fix cycles develops a "clean PRs" belief. An engineer whose PRs frequently need revision gets a "needs review" belief.

These opinions influence review scrutiny — clean PR engineers get lighter reviews focused on architecture, while revision-heavy engineers get closer inspection. Opinions use the existing belief system with confidence scores and natural decay.

## Runtime-Agnostic Personality

Agent personality works identically regardless of which runtime is active. The voice, anti-patterns, beliefs, and communication style are injected into every session — whether it's a persistent Claude tmux session or a fresh Codex process. Swapping runtimes mid-conversation preserves personality because it's in the prompt, not in the session state.
