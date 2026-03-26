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
// Uses time-based decay: conversations stay alive while messages
// are recent, and die naturally when the thread goes quiet.
type ConversationEngine struct {
	agents           map[agent.Role]*agent.Agent
	factStores       map[agent.Role]*memory.FactStore
	projectFactStore *memory.FactStore
	bot              *slack.Bot
	mu               sync.Mutex
	channels         map[string]*channelState
	roster           map[agent.Role]string
	voices           map[agent.Role]string
	pauseChecker     PauseChecker
	concerns         *ConcernTracker
}

type channelState struct {
	recentLines []string
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
		voices:     make(map[agent.Role]string),
	}
}

// SetVoices loads personality voice sections for all agents so chat
// prompts include each agent's distinct communication style.
func (engine *ConversationEngine) SetVoices(loader *agent.PersonalityLoader) {
	if loader == nil {
		return
	}

	engine.mu.Lock()
	defer engine.mu.Unlock()

	for role := range engine.agents {
		engine.voices[role] = loader.LoadVoice(role)
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
func (engine *ConversationEngine) OnMessage(ctx context.Context, channel, sender, text string) {
	engine.OnThreadMessage(ctx, channel, sender, text, "")
}

// OnThreadMessage is like OnMessage but with an explicit thread
// timestamp. When threadTS is non-empty, responses are posted as
// thread replies to that message.
func (engine *ConversationEngine) OnThreadMessage(ctx context.Context, channel, sender, text, threadTS string) {
	engine.mu.Lock()
	state := engine.getOrCreateChannel(channel)

	state.recentLines = appendRecent(state.recentLines, fmt.Sprintf("%s: %s", sender, text))
	timeSinceLast := time.Since(state.lastMessage)
	state.lastMessage = time.Now()

	// Update the active thread when a new threadTS is provided.
	if threadTS != "" {
		state.threadTS = threadTS
	}

	activeThread := state.threadTS
	recentCopy := make([]string, len(state.recentLines))
	copy(recentCopy, state.recentLines)
	engine.mu.Unlock()

	// Mentioned agents always respond, regardless of decay.
	mentioned := engine.findMentionedRoles(recentCopy, sender)

	// Time-based decay: respond if conversation is alive.
	baseCount := decideBaseResponders(timeSinceLast, isHumanMessage(sender))

	// Chitchat is low-priority — max 1 responder so it doesn't
	// dominate agent time or compete with work channels.
	if channel == "chitchat" && baseCount > 1 {
		baseCount = 1
	}

	// Questions always get at least one response.
	if baseCount == 0 && containsQuestion(text) {
		baseCount = 1
	}

	// Mentioned agents bypass decay entirely.
	if baseCount == 0 && len(mentioned) > 0 {
		baseCount = len(mentioned)
	}

	log.Printf("chat: channel=%s sender=%s timeSince=%s responders=%d mentioned=%v thread=%s",
		channel, sender, timeSinceLast.Round(time.Second), baseCount, mentioned, activeThread)

	if baseCount == 0 && len(mentioned) == 0 {
		return
	}

	candidates := engine.pickCandidates(sender, baseCount, recentCopy, mentioned)
	log.Printf("chat: picked %v to respond", candidates)

	var lastResponder string
	for _, role := range candidates {
		log.Printf("chat: %s responding...", role)
		freshLines := engine.RecentMessages(channel)
		engine.tryRespondInThread(ctx, channel, role, freshLines, activeThread)
		lastResponder = string(role)
		log.Printf("chat: %s finished", role)
	}

	// If any response ended with a question, trigger a follow-up round
	// so the question doesn't die unanswered.
	engine.followUpIfQuestion(ctx, channel, lastResponder, activeThread)
}

// followUpIfQuestion checks the most recent message in the channel.
// If an agent just asked a question, pick one responder to answer it.
func (engine *ConversationEngine) followUpIfQuestion(ctx context.Context, channel, lastResponder, threadTS string) {
	recent := engine.RecentMessages(channel)
	if len(recent) == 0 {
		return
	}

	lastLine := recent[len(recent)-1]
	if !containsQuestion(lastLine) {
		return
	}

	log.Printf("chat: last response was a question, triggering follow-up")

	mentioned := engine.findMentionedRoles(recent, lastResponder)
	candidates := engine.pickCandidates(lastResponder, 1, recent, mentioned)
	if len(candidates) == 0 {
		return
	}

	role := candidates[0]
	freshLines := engine.RecentMessages(channel)
	engine.tryRespondInThread(ctx, channel, role, freshLines, threadTS)
}

// IsQuiet returns true if the channel has had no messages for at least
// the given threshold duration.
func (engine *ConversationEngine) IsQuiet(channel string, threshold time.Duration) bool {
	engine.mu.Lock()
	defer engine.mu.Unlock()

	state, ok := engine.channels[channel]
	if !ok {
		return true
	}

	return time.Since(state.lastMessage) >= threshold
}

// BreakSilence is called periodically to have an agent start a
// conversation when channels have been quiet.
func (engine *ConversationEngine) BreakSilence(ctx context.Context) {
	engine.breakSilenceIn(ctx, "engineering", 10*time.Minute)
	// Chitchat only breaks silence when work channels are quiet too —
	// prevents agents chatting when they should be working.
	if engine.IsQuiet("engineering", 5*time.Minute) && engine.IsQuiet("reviews", 5*time.Minute) {
		engine.breakSilenceIn(ctx, "chitchat", 15*time.Minute)
	}
}

func (engine *ConversationEngine) breakSilenceIn(ctx context.Context, channel string, threshold time.Duration) {
	engine.mu.Lock()
	state := engine.getOrCreateChannel(channel)
	timeSinceLast := time.Since(state.lastMessage)
	recentCopy := make([]string, len(state.recentLines))
	copy(recentCopy, state.recentLines)
	engine.mu.Unlock()

	if timeSinceLast < threshold {
		return
	}

	allRoles := agent.AllRoles()
	role := allRoles[rand.IntN(len(allRoles))]
	if role == agent.RoleReviewer {
		return
	}

	engine.tryRespondInThread(ctx, channel, role, recentCopy, "")
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

// ResetRound resets the conversation for a channel by treating it as
// fresh. Kept for API compatibility.
func (engine *ConversationEngine) ResetRound(channel string) {
	engine.mu.Lock()
	defer engine.mu.Unlock()

	state, ok := engine.channels[channel]
	if !ok {
		return
	}
	// Reset by making the last message appear recent.
	state.lastMessage = time.Now()
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

	engine.mu.Lock()
	voiceText := engine.voices[role]
	engine.mu.Unlock()

	prompt := buildChatPrompt(role, channel, recentLines, engine.topBeliefs(ctx, role), engine.roster, voiceText)

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

	// Store strong opinions as beliefs — conversations build memory.
	engine.maybeStoreConversationBelief(ctx, role, text)

	// Track concerns for later investigation during idle time.
	engine.maybeStoreConcerns(role, text)
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
		// Retrieval strengthening — each recall makes the memory stronger.
		_ = factStore.RecordBeliefAccess(ctx, belief.ID)
	}

	return result
}

func (engine *ConversationEngine) pickCandidates(sender string, count int, _ []string, mentioned []agent.Role) []agent.Role {
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

// findMentionedRoles checks the last message for agent names.
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

// DecideBaseRespondersForTest exports decideBaseResponders for testing.
func DecideBaseRespondersForTest(timeSinceNanos int64, isHuman bool) int {
	return decideBaseResponders(time.Duration(timeSinceNanos), isHuman)
}

// FactStores returns the per-agent fact stores for cross-agent queries
// such as the seance.
func (engine *ConversationEngine) FactStores() map[agent.Role]*memory.FactStore {
	return engine.factStores
}

// SetConcernTracker connects the concern tracker so conversation
// responses that contain concern signals are stored for investigation.
func (engine *ConversationEngine) SetConcernTracker(tracker *ConcernTracker) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.concerns = tracker
}

// SetVoicesMap sets the voice text for each role directly. Used in testing.
func (engine *ConversationEngine) SetVoicesMap(voices map[agent.Role]string) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.voices = voices
}

// decideBaseResponders uses time-based decay. Recent messages get full
// engagement, older messages get less, and quiet channels get none.
// Human messages always reset to full engagement.
func decideBaseResponders(timeSinceLast time.Duration, isHuman bool) int {
	if isHuman {
		return 2
	}

	switch {
	case timeSinceLast < 2*time.Minute:
		return 2
	case timeSinceLast < 5*time.Minute:
		return 1
	default:
		return 0
	}
}

func isHumanMessage(sender string) bool {
	for _, role := range agent.AllRoles() {
		if sender == string(role) {
			return false
		}
	}
	return true
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
