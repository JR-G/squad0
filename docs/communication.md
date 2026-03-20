# Communication

## Slack Channels

| Channel | Purpose |
|---------|---------|
| `#commands` | CEO sends structured commands (start, stop, assign, etc.) |
| `#engineering` | Technical discussions, approach debates, implementation questions |
| `#reviews` | PR links, review feedback, CodeRabbit comments |
| `#feed` | Cycle summaries, completed work, retros — the CEO's activity digest |
| `#standup` | Daily standup summaries — CEO can jump in with questions |
| `#triage` | Agent-created tickets for CEO review |

## How Communication Works

**All channels are bidirectional.** The CEO can post in any channel and all agents see it. The natural ones to jump into:
- `#engineering` — weigh in on an approach, redirect the team
- `#reviews` — comment on a PR
- `#standup` — ask follow-up questions, flag priorities
- `#triage` — approve or reject agent-created tickets

**DMs to the bot route to the PM.** The PM is the CEO's primary point of contact — like texting your manager. Use DMs for natural conversation ("what's everyone working on?", "reprioritise auth work") and `#commands` for structured operations.

## Agent Personas

Each agent posts as a distinct persona using Slack's `chat:write.customize` scope. A single bot token varies the display name and avatar per message:

- Display name: the agent's self-chosen name
- Avatar: auto-generated identicon from the agent's name hash

## CEO Commands

Plain text messages in `#commands`. The bot parses the first word as the command:

| Command | Description |
|---------|-------------|
| `start` | Start the orchestrator loop |
| `stop` | Gracefully stop all agents |
| `status` | Show all agent statuses and current work |
| `standup` | Trigger a manual standup |
| `retro` | Trigger a manual retro |
| `assign TICKET agent` | Manually assign a ticket |
| `pause [agent]` | Pause an agent, or all |
| `resume [agent]` | Resume an agent, or all |
| `discuss TICKET` | Trigger a design discussion |
| `agents` | List all agents with models and status |
| `memory agent` | Show an agent's top beliefs |
| `health` | Show agent health states |
| `merge-mode auto\|gated` | Set merge autonomy |
| `version` | Show version |

## Rate Limiting

Slack API allows ~20-30 messages per minute. Squad0 enforces a minimum spacing between posts (default 2 seconds) via a message queue. Messages are never dropped — they queue and drain.

## Agent Discussion Flow

Before implementing a feature, engineers discuss their approach in `#engineering`. The flow is organic — no rigid structure:

1. Assigned engineer posts their planned approach
2. Other agents respond if they have something to add
3. Tech Lead weighs in on architecture
4. Designer critiques UI decisions (for frontend work)
5. The team converges and the engineer starts work

The PM keeps things moving — if a discussion goes in circles, the PM or Tech Lead makes the call.
