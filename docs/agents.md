# Agents

## Roles

| Role | Model | Purpose |
|------|-------|---------|
| PM | Haiku 4.5 | Assigns work, runs rituals, manages board. The CEO's main point of contact. |
| Tech Lead | Opus 4.6 | Reviews architecture, makes technical decisions, final call on debates. |
| Engineer 1 | Sonnet 4.6 | Full-stack, thorough and defensive, leans backend. |
| Engineer 2 | Sonnet 4.6 | Full-stack, fast and pragmatic, leans frontend. |
| Engineer 3 | Sonnet 4.6 | Full-stack, architectural, leans infra/DX. |
| Reviewer | Opus 4.6 | Code review, catches bugs, quality gate. |
| Designer | Sonnet 4.6 | UI/UX critique, consistency, user-centred thinking. |

## Identity System

Agents choose their own names on their first session. The name is stored in their personal knowledge graph as a high-confidence identity fact and used for all Slack messages going forward. There is no CEO veto — agents are free to choose.

Each agent gets a unique identicon avatar generated from a hash of their chosen name.

### How Name Selection Works

1. First session prompt includes: "If you haven't chosen a name yet, pick one now."
2. Agent picks a name that reflects their personality
3. Orchestrator extracts the name and stores it via `PersonaStore.SaveChosenName()`
4. All future sessions load the name from the agent's memory DB
5. Slack messages display the agent's chosen name and identicon

## Personality Evolution

Base personality files (`agents/*.md`) set the starting point — working style, strengths, role boundaries. But the real personality develops through experience.

### How It Works

- Every session, the agent uses MCP memory tools to recall and store knowledge
- Beliefs accumulate with confidence scores that increase with confirmation and decrease with contradictions
- Every N sessions, a personality regeneration step rewrites part of the personality file from accumulated beliefs
- Two agents with identical starting personalities diverge naturally because they work on different tickets

### Example Evolution

An engineer starts with: "You are thorough and defensive."

After 20 sessions, their personality includes:
- "The payments module has fragile error handling around Stripe webhook retries — always add explicit timeout handling there." (confidence: 0.9, confirmed 4 times)
- "Integration tests catch more real bugs than unit tests in this codebase." (confidence: 0.7, confirmed twice)
- "The config loading order matters — always check defaults.go first." (confidence: 0.6)

## Session Lifecycle

1. **Prompt assembly**: load personality + retrieve relevant memories + inject task description
2. **Implementation**: agent works in isolated git worktree, using MCP tools for Linear and memory
3. **Post-session**: orchestrator captures output, stores episode, runs fallback memory extraction

## Ticket Creation

Agents can create Linear tickets during sessions when they discover:
- Bugs: broken functionality outside their current scope
- Tech debt: code that works but violates standards
- Scope overflow: ticket is bigger than expected

The agent should only create tickets for issues that are real, actionable, and outside the scope of their current work. If it can be fixed in under 5 minutes, just fix it.
