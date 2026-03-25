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

// PauseChecker returns true if the given role is currently paused.
type PauseChecker func(ctx context.Context, role agent.Role) bool

// ConversationEngine manages organic agent conversations in Slack.
// Event-driven — triggered by incoming messages, not polling.
type ConversationEngine struct {
	agents       map[agent.Role]*agent.Agent
	factStores   map[agent.Role]*memory.FactStore
	bot          *slack.Bot
	mu           sync.Mutex
	channels     map[string]*channelState
	roster       map[agent.Role]string
	pauseChecker PauseChecker
}

type channelState struct {
	recentLines []string
	roundCount  int
	lastMessage time.Time
	threadTS    string // Slack timestamp of the current conversation thread
}

// NewConversationEngine creates a ConversationEngine.
func NewConversationEngine(
	agents map[agent.Role]*agent.Agent,
	factStores map[agent.Role]*memory.FactStore,
	bot *slack.Bot,
	roster map[agent.Role]string,
) *ConversationEngine {
	return &ConversationEngine{
		agents:     agents,
		factStores: factStores,
		bot:        bot,
		channels:   make(map[string]*channelState),
		roster:     roster,
	}
}

// SetPauseChecker registers a function that the engine calls before
// letting an agent respond. Paused agents are silently skipped.
func (engine *ConversationEngine) SetPauseChecker(checker PauseChecker) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.pauseChecker = checker
}

// UpdateRoster replaces the roster so agents use each other's chosen
// names. Called after introductions or whenever names change.
func (engine *ConversationEngine) UpdateRoster(roster map[agent.Role]string) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.roster = roster
}

// OnMessage is called when any message arrives in a channel.
// It decides if agents should respond and triggers lightweight sessions.
// threadTS is the Slack timestamp of the message that started the
// thread. Pass empty string for non-threaded messages — the engine
// will use the channel's current active thread.
func (engine *ConversationEngine) OnMessage(ctx context.Context, channel, sender, text string) {
	engine.OnThreadMessage(ctx, channel, sender, text, "")
}

// OnThreadMessage is like OnMessage but with an explicit thread
// timestamp. When threadTS is non-empty, responses are posted as
// thread replies to that message.
func (engine *ConversationEngine) OnThreadMessage(ctx context.Context, channel, sender, text, threadTS string) {
	engine.mu.Lock()
	state := engine.getOrCreateChannel(channel)

	timeSinceLast := time.Since(state.lastMessage)
	if timeSinceLast > 5*time.Minute {
		state.roundCount = 0
	}

	state.recentLines = appendRecent(state.recentLines, fmt.Sprintf("%s: %s", sender, text))
	state.lastMessage = time.Now()

	// Update the active thread. Human messages start fresh threads.
	// Agent messages keep the existing thread going.
	shouldUpdateThread := threadTS != "" && (isHumanMessage(sender) || state.threadTS == "")
	if shouldUpdateThread {
		state.threadTS = threadTS
	}

	activeThread := state.threadTS

	state.roundCount = nextRoundCount(sender, state.roundCount)
	roundCount := state.roundCount
	recentCopy := make([]string, len(state.recentLines))
	copy(recentCopy, state.recentLines)
	engine.mu.Unlock()

	respondersCount := decideResponderCount(roundCount)
	log.Printf("chat: channel=%s sender=%s round=%d responders=%d thread=%s",
		channel, sender, roundCount, respondersCount, activeThread)

	if respondersCount == 0 {
		return
	}

	candidates := engine.pickCandidates(sender, respondersCount, recentCopy)
	log.Printf("chat: picked %v to respond", candidates)

	for _, role := range candidates {
		log.Printf("chat: %s responding...", role)
		// Re-read recent lines so each responder sees prior replies.
		freshLines := engine.RecentMessages(channel)
		engine.tryRespondInThread(ctx, channel, role, freshLines, activeThread)
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

	engine.tryRespondInThread(ctx, "engineering", role, recentCopy, "")
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

func (engine *ConversationEngine) tryRespondInThread(ctx context.Context, channel string, role agent.Role, recentLines []string, threadTS string) {
	if engine.isRolePaused(ctx, role) {
		log.Printf("chat: %s is paused, skipping", role)
		return
	}

	agentInstance, ok := engine.agents[role]
	if !ok {
		return
	}

	prompt := buildChatPrompt(role, channel, recentLines, engine.topBeliefs(ctx, role), engine.roster)

	transcript, err := agentInstance.QuickChat(ctx, prompt)
	if err != nil {
		log.Printf("chat failed for %s: %v", role, err)
		return
	}

	text := strings.TrimSpace(transcript)
	log.Printf("chat: %s said: %q", role, text)

	if text == "" || containsPass(text) {
		log.Printf("chat: %s passed or empty", role)
		return
	}

	if engine.bot == nil {
		log.Printf("chat: bot is nil, can't post")
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
}

func (engine *ConversationEngine) postResponse(ctx context.Context, channel, text string, role agent.Role, threadTS string) error {
	if threadTS != "" {
		return engine.bot.PostThreadAsRole(ctx, channel, threadTS, text, role)
	}
	return engine.bot.PostAsRole(ctx, channel, text, role)
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

func (engine *ConversationEngine) pickCandidates(sender string, count int, recentLines []string) []agent.Role {
	mentioned := engine.findMentionedRoles(recentLines, sender)

	allRoles := agent.AllRoles()
	eligible := make([]agent.Role, 0, len(allRoles))
	mentionedSet := make(map[agent.Role]bool, len(mentioned))

	for _, role := range mentioned {
		mentionedSet[role] = true
	}

	for _, role := range allRoles {
		if string(role) == sender || role == agent.RoleReviewer || mentionedSet[role] {
			continue
		}
		eligible = append(eligible, role)
	}

	rand.Shuffle(len(eligible), func(i, j int) {
		eligible[i], eligible[j] = eligible[j], eligible[i]
	})

	// Mentioned agents are guaranteed, then fill remaining slots.
	remaining := count - len(mentioned)
	if remaining < 0 {
		remaining = 0
	}
	if remaining > len(eligible) {
		remaining = len(eligible)
	}

	result := make([]agent.Role, 0, len(mentioned)+remaining)
	result = append(result, mentioned...)
	result = append(result, eligible[:remaining]...)

	return result
}

// findMentionedRoles checks the last message for agent names. Returns
// only the mentioned agents (excluding the sender). When someone says
// "Callum, what do you think?" — only Callum is returned.
func (engine *ConversationEngine) findMentionedRoles(recentLines []string, sender string) []agent.Role {
	if len(recentLines) == 0 {
		return nil
	}

	lastLine := strings.ToLower(recentLines[len(recentLines)-1])

	var mentioned []agent.Role
	for role, name := range engine.roster {
		if name == "" || name == string(role) || string(role) == sender {
			continue
		}
		if strings.Contains(lastLine, strings.ToLower(name)) {
			mentioned = append(mentioned, role)
		}
	}

	return mentioned
}

func buildChatPrompt(role agent.Role, channel string, recentLines, beliefs []string, roster map[agent.Role]string) string {
	var builder strings.Builder

	name := roster[role]
	if name == "" {
		name = string(role)
	}

	builder.WriteString(fmt.Sprintf("Your name is %s. You are %s. ", name, roleDescription(role)))
	builder.WriteString("James is the CEO — he built the team and has final say. When he speaks, pay attention and respond helpfully.")

	if len(beliefs) > 0 {
		builder.WriteString("\n\nThings you believe from experience: ")
		builder.WriteString(strings.Join(beliefs, "; "))
		builder.WriteString(".")
	}

	writeRoster(&builder, role, roster)

	fmt.Fprintf(&builder, "\n\nConversation in #%s:\n", channel)

	if len(recentLines) == 0 {
		builder.WriteString("(quiet — nothing recent)\n")
	}

	for _, line := range recentLines {
		fmt.Fprintf(&builder, "> %s\n", line)
	}

	lastMessage := ""
	if len(recentLines) > 0 {
		lastMessage = recentLines[len(recentLines)-1]
	}

	builder.WriteString("\nRespond to this conversation. You're talking to real teammates.")

	if lastMessage != "" {
		fmt.Fprintf(&builder, " The most recent message is: \"%s\" — engage with it directly.", lastMessage)
	}

	builder.WriteString(" Use people's names. Ask follow-up questions. Disagree if you disagree. Build on their ideas. Be yourself — your tone, your perspective, your opinions.")
	builder.WriteString("\n\nKeep it to 1-3 sentences. Respond with ONLY what you'd type in Slack.")
	builder.WriteString("\nNEVER include meta-commentary, parenthetical notes, stage directions, or alternatives.")
	builder.WriteString("\nNEVER break character or mention being an AI.")
	builder.WriteString("\nIf you genuinely have nothing to add, respond with exactly: PASS")

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

func writeRoster(builder *strings.Builder, self agent.Role, roster map[agent.Role]string) {
	if len(roster) == 0 {
		return
	}

	builder.WriteString("\n\nYour team: ")
	rosterParts := make([]string, 0, len(roster))
	for role, name := range roster {
		if role != self {
			rosterParts = append(rosterParts, fmt.Sprintf("%s (%s)", name, roleTitle(role)))
		}
	}
	builder.WriteString(strings.Join(rosterParts, ", "))
	builder.WriteString(". Use their names, not role IDs.")
}

func roleTitle(role agent.Role) string {
	switch role {
	case agent.RolePM:
		return "PM"
	case agent.RoleTechLead:
		return "Tech Lead"
	case agent.RoleEngineer1, agent.RoleEngineer2, agent.RoleEngineer3:
		return "Engineer"
	case agent.RoleReviewer:
		return "Reviewer"
	case agent.RoleDesigner:
		return "Designer"
	}
	return string(role)
}

func decideResponderCount(roundCount int) int {
	switch {
	case roundCount <= 2:
		return 2
	case roundCount <= 5:
		return 1
	case roundCount <= 8:
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

func containsPass(text string) bool {
	upper := strings.ToUpper(text)
	return strings.Contains(upper, "PASS")
}

func (engine *ConversationEngine) isRolePaused(ctx context.Context, role agent.Role) bool {
	if engine.pauseChecker == nil {
		return false
	}
	return engine.pauseChecker(ctx, role)
}

func appendRecent(lines []string, line string) []string {
	maxRecent := 15
	lines = append(lines, line)
	if len(lines) > maxRecent {
		lines = lines[len(lines)-maxRecent:]
	}
	return lines
}
