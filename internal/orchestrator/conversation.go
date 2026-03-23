package orchestrator

import (
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	slack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/JR-G/squad0/internal/memory"
)

// ConversationEngine manages organic agent conversations in Slack.
// Event-driven — triggered by incoming messages, not polling.
type ConversationEngine struct {
	agents     map[agent.Role]*agent.Agent
	factStores map[agent.Role]*memory.FactStore
	bot        *slack.Bot
	mu         sync.Mutex
	channels   map[string]*channelState
}

type channelState struct {
	recentLines []string
	roundCount  int
	lastMessage time.Time
}

// NewConversationEngine creates a ConversationEngine.
func NewConversationEngine(
	agents map[agent.Role]*agent.Agent,
	factStores map[agent.Role]*memory.FactStore,
	bot *slack.Bot,
) *ConversationEngine {
	return &ConversationEngine{
		agents:     agents,
		factStores: factStores,
		bot:        bot,
		channels:   make(map[string]*channelState),
	}
}

// OnMessage is called when any message arrives in a channel.
// It decides if agents should respond and triggers lightweight sessions.
func (engine *ConversationEngine) OnMessage(ctx context.Context, channel, sender, text string) {
	engine.mu.Lock()
	state := engine.getOrCreateChannel(channel)

	timeSinceLast := time.Since(state.lastMessage)
	if timeSinceLast > 5*time.Minute {
		state.roundCount = 0
	}

	state.recentLines = appendRecent(state.recentLines, fmt.Sprintf("%s: %s", sender, text))
	state.lastMessage = time.Now()

	state.roundCount = nextRoundCount(sender, state.roundCount)
	roundCount := state.roundCount
	recentCopy := make([]string, len(state.recentLines))
	copy(recentCopy, state.recentLines)
	engine.mu.Unlock()

	respondersCount := decideResponderCount(roundCount)
	log.Printf("chat: channel=%s sender=%s round=%d responders=%d", channel, sender, roundCount, respondersCount)

	if respondersCount == 0 {
		return
	}

	candidates := engine.pickCandidates(sender, respondersCount)
	log.Printf("chat: picked %v to respond", candidates)

	for _, role := range candidates {
		log.Printf("chat: %s responding...", role)
		engine.tryRespond(ctx, channel, role, recentCopy)
		log.Printf("chat: %s finished", role)
	}
}

// BreakSilence is called periodically to have an agent start a
// conversation when channels have been quiet.
func (engine *ConversationEngine) BreakSilence(ctx context.Context) {
	engine.mu.Lock()
	state := engine.getOrCreateChannel("engineering")
	timeSinceLast := time.Since(state.lastMessage)
	recentCopy := make([]string, len(state.recentLines))
	copy(recentCopy, state.recentLines)
	engine.mu.Unlock()

	if timeSinceLast < 10*time.Minute {
		return
	}

	allRoles := agent.AllRoles()
	role := allRoles[rand.IntN(len(allRoles))]
	if role == agent.RoleReviewer {
		return
	}

	engine.tryRespond(ctx, "engineering", role, recentCopy)
}

// RecentMessages returns the conversation context for a channel.
func (engine *ConversationEngine) RecentMessages(channel string) []string {
	engine.mu.Lock()
	defer engine.mu.Unlock()

	state, ok := engine.channels[channel]
	if !ok {
		return nil
	}

	result := make([]string, len(state.recentLines))
	copy(result, state.recentLines)
	return result
}

// ResetRound resets the conversation round counter for a channel.
func (engine *ConversationEngine) ResetRound(channel string) {
	engine.mu.Lock()
	defer engine.mu.Unlock()

	state, ok := engine.channels[channel]
	if !ok {
		return
	}
	state.roundCount = 0
}

// SetLastMessageTime sets the last message time for a channel. Used in
// testing to simulate quiet periods.
func (engine *ConversationEngine) SetLastMessageTime(channel string, lastMessage time.Time) {
	engine.mu.Lock()
	defer engine.mu.Unlock()

	state := engine.getOrCreateChannel(channel)
	state.lastMessage = lastMessage
}

func (engine *ConversationEngine) getOrCreateChannel(channel string) *channelState {
	state, ok := engine.channels[channel]
	if !ok {
		state = &channelState{lastMessage: time.Now()}
		engine.channels[channel] = state
	}
	return state
}

func (engine *ConversationEngine) tryRespond(ctx context.Context, channel string, role agent.Role, recentLines []string) {
	agentInstance, ok := engine.agents[role]
	if !ok {
		return
	}

	prompt := buildChatPrompt(role, channel, recentLines, engine.topBeliefs(ctx, role))

	transcript, err := agentInstance.QuickChat(ctx, prompt)
	if err != nil {
		log.Printf("chat failed for %s: %v", role, err)
		return
	}

	text := strings.TrimSpace(transcript)
	log.Printf("chat: %s said: %q", role, text)

	if text == "" || strings.EqualFold(text, "PASS") {
		log.Printf("chat: %s passed or empty", role)
		return
	}

	if engine.bot == nil {
		log.Printf("chat: bot is nil, can't post")
		return
	}

	err = engine.bot.PostAsRole(ctx, channel, text, role)
	if err != nil {
		log.Printf("chat: failed to post for %s in %s: %v", role, channel, err)
		return
	}

	engine.mu.Lock()
	state := engine.getOrCreateChannel(channel)
	state.recentLines = appendRecent(state.recentLines, fmt.Sprintf("%s: %s", role, text))
	engine.mu.Unlock()
}

func (engine *ConversationEngine) topBeliefs(ctx context.Context, role agent.Role) []string {
	factStore, ok := engine.factStores[role]
	if !ok {
		return nil
	}

	beliefs, err := factStore.TopBeliefs(ctx, 5)
	if err != nil {
		return nil
	}

	result := make([]string, 0, len(beliefs))
	for _, belief := range beliefs {
		result = append(result, belief.Content)
	}

	return result
}

func (engine *ConversationEngine) pickCandidates(sender string, count int) []agent.Role {
	allRoles := agent.AllRoles()
	eligible := make([]agent.Role, 0, len(allRoles))

	for _, role := range allRoles {
		if string(role) == sender {
			continue
		}
		if role == agent.RoleReviewer {
			continue
		}
		eligible = append(eligible, role)
	}

	rand.Shuffle(len(eligible), func(i, j int) {
		eligible[i], eligible[j] = eligible[j], eligible[i]
	})

	if count > len(eligible) {
		count = len(eligible)
	}

	return eligible[:count]
}

func buildChatPrompt(role agent.Role, channel string, recentLines, beliefs []string) string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("You are %s. ", roleDescription(role)))

	if len(beliefs) > 0 {
		builder.WriteString("Things you believe from experience: ")
		builder.WriteString(strings.Join(beliefs, "; "))
		builder.WriteString(". ")
	}

	fmt.Fprintf(&builder, "\n\nRecent messages in #%s:\n", channel)

	if len(recentLines) == 0 {
		builder.WriteString("(quiet — nothing recent)\n")
	}

	for _, line := range recentLines {
		fmt.Fprintf(&builder, "> %s\n", line)
	}

	builder.WriteString("\nYou're part of this team. Respond naturally — react to what's been said, share your perspective, ask a question, or build on the conversation. Be yourself. Keep it to 1-2 sentences. Only respond with PASS if the conversation has clearly ended and there's genuinely nothing left to say.")

	return builder.String()
}

func roleDescription(role agent.Role) string {
	switch role {
	case agent.RolePM:
		return "the PM — you keep the team focused and unblocked"
	case agent.RoleTechLead:
		return "the tech lead — you think in systems and care about architecture"
	case agent.RoleEngineer1:
		return "an engineer — thorough, defensive, backend-leaning"
	case agent.RoleEngineer2:
		return "an engineer — fast, pragmatic, frontend-leaning"
	case agent.RoleEngineer3:
		return "an engineer — architectural, infra and DX focused"
	case agent.RoleReviewer:
		return "the reviewer — you catch bugs and ensure quality"
	case agent.RoleDesigner:
		return "the designer — you think from the user's perspective"
	}
	return string(role)
}

func decideResponderCount(roundCount int) int {
	switch {
	case roundCount <= 1:
		return 2
	case roundCount <= 3:
		return 1
	case roundCount <= 5:
		if rand.Float64() < 0.5 {
			return 1
		}
		return 0
	default:
		if rand.Float64() < 0.2 {
			return 1
		}
		return 0
	}
}

func nextRoundCount(sender string, current int) int {
	if isHumanMessage(sender) {
		return 0
	}
	return current + 1
}

func isHumanMessage(sender string) bool {
	for _, role := range agent.AllRoles() {
		if sender == string(role) {
			return false
		}
	}
	return true
}

func appendRecent(lines []string, line string) []string {
	maxRecent := 15
	lines = append(lines, line)
	if len(lines) > maxRecent {
		lines = lines[len(lines)-maxRecent:]
	}
	return lines
}
