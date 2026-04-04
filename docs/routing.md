# Routing and Intelligence

Squad0 uses intelligent routing to match model power to task complexity, skip unnecessary discussions, track agent specialisations, form inter-agent opinions, map arguments in discussions, and budget token spend.

## Semantic Model Routing

Not every ticket needs Opus. The `ComplexityClassifier` analyses ticket metadata and routes to the appropriate model:

| Complexity | Model | Signals |
|-----------|-------|---------|
| Trivial | Haiku | Labels: chore, docs, style, rename. Description < 200 chars. |
| Standard | Sonnet | Everything else. The default. |
| Complex | Opus | Labels: architecture, security, migration, epic. Description > 1000 chars. |

Classification is heuristic — no LLM calls. The classifier reads ticket title, description, and labels.

## Adaptive Discussion Depth

Discussion depth maps directly from complexity:

| Complexity | Depth | Behaviour |
|-----------|-------|-----------|
| Trivial | None | Skip discussion entirely. Engineer starts coding immediately. |
| Standard | Light | Tech Lead reviews the plan. No PM tie-break, no team debate. |
| Complex | Full | Current ritual: plan, tech lead review, team discussion, PM tie-break. |

This means simple bug fixes don't waste tokens on team debate, while architectural changes get the full treatment.

## Emergent Specialisation

The `SpecialisationStore` tracks agent success rates per ticket category (extracted from Linear labels). Over time, the PM's assignment becomes smarter:

```
Engineer-1: auth (85% success), backend (70%), frontend (40%)
Engineer-2: frontend (90% success), UI (85%), backend (60%)
Engineer-3: infra (95% success), CI (90%), backend (75%)
```

- Outcomes recorded after each session (success or failure)
- Categories come from ticket labels
- Minimum 2 attempts before a specialisation score is used
- The PM assignment weighs specialisation scores alongside existing label matching

This isn't hardcoded — "Engineer-1 leans backend" evolves into data-driven "Engineer-1 has 85% success on auth tickets." The specialisation emerges from actual work, not from personality files.

## Inter-Agent Opinions

Agents form beliefs about each other's work quality. These are stored as beliefs in the existing `FactStore` with a naming convention:

```
[about:engineer-1] clean PRs          (confidence: 0.7)
[about:engineer-2] PRs need revision   (confidence: 0.6)
```

Opinions form from review outcomes:
- Approved with 0 fix cycles → positive belief ("clean PRs")
- Multiple fix cycles → negative belief ("PRs need revision")
- Standard outcomes → neutral

The reviewer uses opinions to adjust scrutiny:
- **Low scrutiny** (consistently clean PRs): "Focus on architecture over nitpicks"
- **Normal scrutiny**: default review depth
- **High scrutiny** (frequent revisions): "Review with extra care"

Opinions use the existing belief system — they have confidence scores, decay over time, and strengthen with confirmation.

## Argument Mapping

During team discussions, the `ArgumentMap` captures structured positions instead of raw message quoting:

```
## Discussion Summary

*Positions:*
- Callum: use a single database with views
- Sable: separate service with event sync

*Evidence:*
- benchmarks show 2x throughput with views

*Unresolved Concerns:*
- CI pipeline needs changes for the service approach

*Decision:* single database with views
```

This replaces the old approach of dumping raw Slack messages into the implementation prompt. The engineer gets a structured understanding of what was debated and decided.

Messages are auto-classified using keyword heuristics:
- **Positions**: "I think", "we should", "my approach"
- **Concerns**: "concerned about", "what happens when", "the risk is"
- **Evidence**: "because", "last time we", "data shows"

## Cost-Aware Token Budgeting

The `TokenLedger` tracks spend per ticket and per agent daily:

```toml
[agents.budget]
max_tokens_per_ticket = 500000      # 0 = no limit
max_tokens_per_agent_daily = 2000000
```

When a ticket exceeds its budget, the sensor detects it and pushes a situation to the PM. The PM can decide to:
- Continue (override the budget)
- Block the ticket
- Reassign to a cheaper model

Token counts are extracted from Claude Code's stream-json usage messages in `SessionResult.TokensUsed`.

## Package Structure

All routing code lives in `internal/routing/`:

```
internal/routing/
  complexity.go        — ComplexityClassifier
  depth.go             — DepthClassifier
  specialisation.go    — SpecialisationStore (SQLite)
  opinions.go          — OpinionStore (wraps FactStore)
  budget.go            — TokenLedger
```
