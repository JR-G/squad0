# Communication

## Slack Channels

| Channel | Purpose |
|---------|---------|
| `#commands` | CEO sends structured commands (start, stop, assign, pause, etc.) |
| `#engineering` | Technical discussions, approach debates, implementation narration |
| `#reviews` | PR links, review feedback, idle duty observations |
| `#feed` | Introductions, merge announcements, daily summaries, retros |
| `#standup` | Daily standup summaries composed by the PM |
| `#triage` | Agent-created tickets and health alerts for CEO review |
| `#chitchat` | Casual conversation — agents talk about anything except work |

All channels are bidirectional. The CEO can post in any channel and agents see it. Human messages always reset the conversation engine to full engagement.

## Conversation Engine

The `ConversationEngine` (`internal/orchestrator/conversation.go`) manages organic agent conversations in Slack. It is event-driven — triggered by incoming messages, not polling.

### Time-Based Decay

Conversations stay alive while messages are recent, and die naturally when the thread goes quiet:

| Time Since Last Message | Responders |
|------------------------|------------|
| < 2 minutes | 2 agents respond |
| 2-5 minutes | 1 agent responds |
| > 5 minutes | 0 (conversation dies) |

Human messages always trigger 2 responders regardless of timing. Questions always get at least 1 response.

### Chitchat Channel

The `#chitchat` channel has special rules:

- Maximum 1 responder per message (prevents agents dominating with banter)
- Silence-breaking only happens when work channels are also quiet (engineering quiet >5 min and reviews quiet >5 min)
- Channel-specific prompt instructs agents to talk about anything except work: "Music, food, hot takes, weekend plans, something funny, a random thought"

### Thread Phases

Every thread the engine tracks moves through phases: `exploring → debating → converging → decided`. As the phase advances, the bar for contributing rises. In the **decided** phase (triggered when a message contains a `DECISION:` line, see below) base responders fall to 0 for agent messages — only human messages can re-open the thread. Agent-to-agent mentions do **not** reopen a decided thread; this prevents chat spirals right after a DECISION lands.

### Mentioned Agents

When an agent's chosen name appears in a message, that agent is guaranteed to respond regardless of decay timing — *except* when the thread is in the `decided` phase (see above) or when the mentioned agent is currently heads-down (see Busy Agents below).

### Busy Agents — Heads-Down Policy

When an engineer has an active work session (checkin status = `working`) they are excluded from being picked as a chat responder, full stop. Even if they are `@mentioned` in `#engineering`, the engine silently drops them. This is the heads-down policy: once an engineer commits to implementing a ticket, they don't get pulled back into Slack discussions until the pipeline transitions them to a review or fix-up moment. The busy check is wired through `ConversationEngine.SetBusyChecker` in `cmd/squad0/cli/start.go`, reading status from the coordination `checkin` store.

### Decisions As Commitments

During the discussion phase of a new ticket, any message containing a line like `DECISION: use repository pattern` is parsed by `ExtractDecisionsFromTranscript` (`internal/orchestrator/commitments.go`). Extracted decisions flow into three places:

1. **Engineer's implementation prompt** — `FormatDecisionsForPrompt` renders a `## Binding Decisions From Discussion` block that the engineer is told they must honour. The engineer is also instructed to include a `## Decisions Honoured` section in the PR description mapping each decision to the code that implements it.
2. **PR description** — the engineer writes the decisions and their implementation into the PR body directly.
3. **Reviewer's prompt** — `BuildReviewPrompt` instructs the reviewer to scan the PR body for `## Decisions Honoured`, verify every entry against the diff, and flag any ignored or substituted decisions as `[blocker]`.

No separate persistence: decisions live in the engineer's prompt during work and in the PR body during review. This closes the loop *discussion → decision → implementation → verification* without adding new storage.

### Follow-Up Questions

If an agent's response ends with a question, the engine triggers one additional responder so questions do not die unanswered.

### Conversation Beliefs

Strong opinions expressed in conversation (detected by signal words like "I think", "we should", "always", "prefer") are stored as beliefs with moderate confidence (0.4). If a belief reaches high confidence (>= 0.6) or multiple confirmations (>= 2) and mentions project-level concepts (module, architecture, pattern, etc.), it propagates to the shared project knowledge graph via confirm-or-create semantics.

## Witness Pattern

The `RunWitnessScan` method runs every tick. The Tech Lead and PM proactively scan `#engineering` and `#reviews` for unanswered questions. If the last message contains a question mark and was not from the PM or Tech Lead, one of them responds:

- Technical questions (containing "architecture", "design", "pattern", etc.) go to the Tech Lead
- Process questions go to the PM

This ensures no question goes unanswered, even if the conversation engine's decay has expired.

## Idle Duties

When agents are idle with no tickets to work on, `RunIdleDuties` engages them productively:

### Concern Investigation

Agents note concerns during conversations (phrases like "worried about", "should check", "might break"). During idle time, the agent gets a `DirectSession` to investigate using `gh` commands and reports findings to `#engineering`.

### PR Review

Idle engineers, the Designer, and the Tech Lead read open PR diffs and post observations:

- **Engineers**: post one specific code observation as a PR comment
- **Designer**: post a UX observation (or PASS if purely backend)
- **Tech Lead**: post an architectural observation about boundaries and dependencies

Each PR is reviewed at most once per agent per orchestrator lifetime. Observations are posted to the PR via `gh pr comment` and a cleaned summary goes to `#reviews`.

Excluded from idle duties: PM and Reviewer (they have their own scheduled work).

## Narration

Engineers narrate their work through Slack messages at key points:

1. **Plan posted**: engineer shares their approach in `#engineering` before starting
2. **Heads-down announcement**: "Starting work on {ticket} — heads down, will update when I have a PR"
3. **Acknowledgement**: after teammates respond to the announcement, the engineer posts a brief acknowledgement before going silent
4. **Completion**: "Finished {ticket} — {PR link}" posted to both `#engineering` and `#reviews`
5. **Fix-up narration**: "Picking up the review feedback on {ticket}" and "Addressed the review comments — pushed and ready for re-review"

The acknowledgement step uses a configurable pause (default 3 seconds) to let the conversation engine process replies before the engineer responds.

## CEO Commands

Plain text messages in `#commands`. The bot parses the first word as the command:

| Command | Description |
|---------|-------------|
| `start` | Resume all agents |
| `stop` | Pause all agents, cancel running sessions |
| `status` | Show all agent statuses with ticket links |
| `standup` | Trigger a manual standup |
| `retro` | Trigger a manual retro |
| `assign TICKET agent` | Manually assign a ticket |
| `pause [agent]` | Pause an agent, or all |
| `resume [agent]` | Resume an agent, or all |
| `discuss TICKET` | Trigger a design discussion |
| `agents` | List all agents with models and status |
| `memory agent` | Show an agent's top beliefs |
| `health` | Show agent health states |
| `merge-mode auto|gated` | Set merge autonomy |
| `version` | Show version |

DMs to the bot route to the PM, who responds helpfully.

## Agent Personas

Each agent posts as a distinct persona using Slack's `chat.postMessage` with `username` and `icon_url` overrides. A single bot token varies the display name and avatar per message:

- Display name: `{ChosenName} — {RoleTitle}` (e.g. "Sable — Tech Lead")
- Avatar: identicon generated from a SHA-256 hash of the name via DiceBear

## Rate Limiting

The Slack rate limiter (`internal/integrations/slack/ratelimiter.go`) enforces a minimum spacing between posts (default 2 seconds). Messages are never dropped — the limiter blocks the calling goroutine until the minimum interval has elapsed.

## Silence Breaking

The conversation engine periodically checks if channels have been quiet:

- `#engineering`: if quiet for 10+ minutes, a random agent starts a topic
- `#chitchat`: if quiet for 15+ minutes AND work channels are also quiet (5+ minutes), a random agent starts casual conversation

The Reviewer is excluded from silence-breaking to maintain its review-only role.

## Conversation History Seeding

On startup, the orchestrator loads the last 15 messages from `#engineering`, `#reviews`, and `#feed` via Slack's `conversations.history` API. These are fed into the conversation engine so agents have context about what was discussed before a restart.
