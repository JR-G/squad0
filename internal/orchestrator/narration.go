package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/JR-G/squad0/internal/agent"
)

const defaultAcknowledgePause = 3 * time.Second

func (orch *Orchestrator) acknowledgePause() time.Duration {
	if orch.cfg.AcknowledgePause > 0 {
		return orch.cfg.AcknowledgePause
	}
	return defaultAcknowledgePause
}

// AcknowledgeThreadForTest exports acknowledgeThread for testing.
func (orch *Orchestrator) AcknowledgeThreadForTest(ctx context.Context, agentInstance *agent.Agent, role agent.Role, channel string) {
	orch.acknowledgeThread(ctx, agentInstance, role, channel)
}

// acknowledgeThread checks if teammates responded to the engineer's
// narration and posts a quick acknowledgment before going heads-down.
func (orch *Orchestrator) acknowledgeThread(ctx context.Context, agentInstance *agent.Agent, role agent.Role, channel string) {
	if orch.conversation == nil {
		return
	}

	lines := orch.conversation.RecentMessages(channel)
	if len(lines) < 2 {
		return
	}

	// Only acknowledge if someone else responded after us.
	lastLine := lines[len(lines)-1]
	if strings.Contains(lastLine, string(role)) {
		return
	}

	// Use QuickChat for a natural response — personality comes through.
	tail := lines
	if len(tail) > 3 {
		tail = tail[len(tail)-3:]
	}

	prompt := fmt.Sprintf("Reply to your teammates with a quick acknowledgment (1 sentence) before diving into work:\n\n%s",
		strings.Join(tail, "\n"))
	response, err := agentInstance.QuickChat(ctx, prompt)
	if err != nil {
		return
	}

	response = filterPassResponse(response)
	if response != "" {
		orch.postAsRole(ctx, channel, response, role)
	}
}
