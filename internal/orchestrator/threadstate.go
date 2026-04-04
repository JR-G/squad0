package orchestrator

import (
	"strings"
	"sync"
	"time"

	"github.com/JR-G/squad0/internal/agent"
)

// ThreadPhase represents where a conversation is in its lifecycle.
type ThreadPhase string

const (
	// PhaseExploring is the default — open discussion, ideas welcome.
	PhaseExploring ThreadPhase = "exploring"
	// PhaseDebating means alternatives are being weighed.
	PhaseDebating ThreadPhase = "debating"
	// PhaseConverging means the team is aligning on a direction.
	PhaseConverging ThreadPhase = "converging"
	// PhaseDecided means a decision was made. High bar for new input.
	PhaseDecided ThreadPhase = "decided"
)

// ThreadState tracks the conversational dynamics of a single thread.
type ThreadState struct {
	Phase        ThreadPhase
	TurnCount    int
	Participants map[agent.Role]int // role → message count
	KeyPoints    []string           // unique points raised
	Decision     string             // extracted decision, if any
	LastUpdate   time.Time
}

// ThreadTracker manages thread states across channels. Each channel
// tracks its active thread independently.
type ThreadTracker struct {
	mu      sync.Mutex
	threads map[string]*ThreadState // channel → state
}

// NewThreadTracker creates a ThreadTracker.
func NewThreadTracker() *ThreadTracker {
	return &ThreadTracker{
		threads: make(map[string]*ThreadState),
	}
}

// Update processes a new message and advances the thread state.
func (tracker *ThreadTracker) Update(channel string, sender agent.Role, text string) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	state := tracker.getOrCreate(channel)
	state.TurnCount++
	state.LastUpdate = time.Now()
	state.Participants[sender]++

	// Extract key points — short, unique contributions.
	point := extractKeyPoint(text)
	if point != "" && !containsPoint(state.KeyPoints, point) {
		state.KeyPoints = append(state.KeyPoints, point)
	}

	// Check for an explicit decision.
	if decision := extractDecision(text); decision != "" {
		state.Decision = decision
		state.Phase = PhaseDecided
		return
	}

	// Advance phase based on content signals.
	state.Phase = classifyPhase(state, text)
}

// Get returns the current state for a channel thread.
func (tracker *ThreadTracker) Get(channel string) ThreadState {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	state, ok := tracker.threads[channel]
	if !ok {
		return ThreadState{Phase: PhaseExploring}
	}

	return *state
}

// Reset clears the thread state for a channel. Called when a thread
// dies naturally or a new topic starts.
func (tracker *ThreadTracker) Reset(channel string) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	delete(tracker.threads, channel)
}

func (tracker *ThreadTracker) getOrCreate(channel string) *ThreadState {
	state, ok := tracker.threads[channel]
	if !ok {
		state = &ThreadState{
			Phase:        PhaseExploring,
			Participants: make(map[agent.Role]int),
			LastUpdate:   time.Now(),
		}
		tracker.threads[channel] = state
	}
	return state
}

// classifyPhase determines the thread phase from accumulated signals.
// Phases only move forward (exploring → debating → converging) unless
// a new point of disagreement reopens debate.
func classifyPhase(state *ThreadState, latestText string) ThreadPhase {
	// Already decided — stay decided.
	if state.Phase == PhaseDecided {
		return PhaseDecided
	}

	lower := strings.ToLower(latestText)

	// Convergence signals — agreement, alignment.
	if hasConvergenceSignal(lower) && state.TurnCount >= 3 {
		return PhaseConverging
	}

	// Debate signals — alternatives, disagreement, counterpoints.
	if hasDebateSignal(lower) {
		return PhaseDebating
	}

	// After several turns with multiple participants but no convergence,
	// it's debating even without explicit signals.
	implicitDebate := state.TurnCount >= 4 && len(state.Participants) >= 2 && state.Phase == PhaseExploring
	if implicitDebate {
		return PhaseDebating
	}

	// Preserve current phase if no new signals.
	if state.Phase != PhaseExploring {
		return state.Phase
	}

	return PhaseExploring
}

var convergenceSignals = []string{
	"agreed",
	"makes sense",
	"let's go with",
	"sounds good",
	"that works",
	"+1",
	"on board with",
	"I'm convinced",
	"fair enough",
	"sold",
}

var debateSignals = []string{
	"alternatively",
	"on the other hand",
	"I disagree",
	"not sure about",
	"the problem with that",
	"what about instead",
	"I'd push back",
	"counterpoint",
	"but what if",
	"not convinced",
}

func hasConvergenceSignal(lower string) bool {
	for _, signal := range convergenceSignals {
		if strings.Contains(lower, signal) {
			return true
		}
	}
	return false
}

func hasDebateSignal(lower string) bool {
	for _, signal := range debateSignals {
		if strings.Contains(lower, signal) {
			return true
		}
	}
	return false
}

// extractDecision looks for an explicit "DECISION:" line.
func extractDecision(text string) string {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		upper := strings.ToUpper(trimmed)
		if !strings.HasPrefix(upper, "DECISION:") {
			continue
		}
		// Return the content after "DECISION:"
		idx := strings.Index(upper, "DECISION:") + len("DECISION:")
		return strings.TrimSpace(trimmed[idx:])
	}
	return ""
}

// extractKeyPoint pulls the core assertion from a message. Returns
// empty for very short or low-information messages.
func extractKeyPoint(text string) string {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) < 15 {
		return ""
	}
	// Use the first sentence as the key point.
	for idx, ch := range trimmed {
		isSentenceEnd := ch == '.' || ch == '!' || ch == '?'
		if isSentenceEnd && idx > 10 {
			return trimmed[:idx+1]
		}
	}
	// No sentence-ending punctuation — use the whole thing if short.
	if len(trimmed) <= 120 {
		return trimmed
	}
	return trimmed[:120]
}

func containsPoint(points []string, point string) bool {
	lowerPoint := strings.ToLower(point)
	for _, existing := range points {
		if strings.ToLower(existing) == lowerPoint {
			return true
		}
	}
	return false
}

// PromptForPhase returns a phase-appropriate instruction to include
// in the chat prompt. This is the core of progressive prompting —
// the bar for contributing rises as the thread matures.
func PromptForPhase(phase ThreadPhase, state ThreadState) string {
	switch phase {
	case PhaseExploring:
		return "The team is discussing this. Share your perspective if you have one."

	case PhaseDebating:
		pointsSummary := summarisePoints(state.KeyPoints)
		if pointsSummary != "" {
			return "The thread is weighing options. Points raised so far: " +
				pointsSummary +
				"\nIf you have a *new* angle or a strong opinion, weigh in. If you'd just be echoing what's been said, PASS."
		}
		return "The thread is weighing options. If you have a *new* angle or a strong opinion, weigh in. If you'd just be echoing what's been said, PASS."

	case PhaseConverging:
		return "The team is aligning. Only respond if you see a specific problem that hasn't been raised. Agreement doesn't need restating."

	case PhaseDecided:
		if state.Decision != "" {
			return "Decision reached: " + state.Decision + ". Only respond if you spot a critical issue that was missed."
		}
		return "A decision was reached. Only respond if you spot a critical issue that was missed."
	}

	return ""
}

func summarisePoints(points []string) string {
	if len(points) == 0 {
		return ""
	}
	if len(points) > 4 {
		points = points[len(points)-4:]
	}
	return strings.Join(points, "; ")
}
