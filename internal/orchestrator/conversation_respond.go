package orchestrator

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
)

// generateValidResponse calls QuickChat, validates the output, and
// retries once if the first attempt is rejected. Returns empty if
// both attempts fail.
func (engine *ConversationEngine) generateValidResponse(ctx context.Context, agentInstance *agent.Agent, role agent.Role, prompt string, recentLines []string) string {
	text, ok := engine.attemptChat(ctx, agentInstance, role, prompt, recentLines)
	if ok {
		return text
	}

	// Retry once — the model might self-correct.
	log.Printf("chat: %s retrying after rejection", role)
	text, ok = engine.attemptChat(ctx, agentInstance, role, prompt, recentLines)
	if ok {
		return text
	}

	log.Printf("chat: %s retry also rejected — dropping", role)
	return ""
}

func (engine *ConversationEngine) attemptChat(ctx context.Context, agentInstance *agent.Agent, role agent.Role, prompt string, recentLines []string) (string, bool) {
	transcript, err := agentInstance.QuickChat(ctx, prompt)
	if err != nil {
		log.Printf("chat failed for %s: %v", role, err)
		return "", false
	}

	text := strings.TrimSpace(transcript)
	log.Printf("chat: %s said: %q", role, text)

	if text == "" || containsPass(text) {
		log.Printf("chat: %s passed or empty", role)
		return "", false
	}

	if engine.outputPipeline == nil {
		return text, true
	}

	validated, result := engine.outputPipeline.Process(text, role, recentLines)
	if !result.OK {
		log.Printf("chat: %s rejected: %s", role, result.Reason)
		return "", false
	}

	return validated, true
}

func (engine *ConversationEngine) postAndRecord(ctx context.Context, channel string, role agent.Role, text, threadTS string) {
	if engine.bot == nil {
		return
	}

	postErr := engine.postResponse(ctx, channel, text, role, threadTS)
	if postErr != nil {
		log.Printf("chat: failed to post for %s in %s: %v", role, channel, postErr)
		return
	}

	engine.mu.Lock()
	state := engine.getOrCreateChannel(channel)
	state.recentLines = appendRecent(state.recentLines, fmt.Sprintf("%s: %s", role, text))
	engine.mu.Unlock()

	engine.maybeStoreConversationBelief(ctx, role, text)
	engine.maybeStoreConcerns(role, text)
}
