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

### Mentioned Agents

When an agent's chosen name appears in a message, that agent is guaranteed to respond regardless of decay timing. Mentioned agents bypass the responder count limits.

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
