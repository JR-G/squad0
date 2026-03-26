package orchestrator

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/JR-G/squad0/internal/agent"
)

// AgentConcern represents something an agent noticed but hasn't
// verified yet. During idle time, agents investigate their concerns.
type AgentConcern struct {
	Role      agent.Role
	Content   string
	Ticket    string
	CreatedAt time.Time
	Resolved  bool
}

// ConcernTracker stores and manages agent concerns. Thread-safe for
// concurrent access from multiple agent goroutines.
type ConcernTracker struct {
	mu       sync.Mutex
	concerns []AgentConcern
}

// NewConcernTracker creates an empty ConcernTracker.
func NewConcernTracker() *ConcernTracker {
	return &ConcernTracker{}
}

// concernSignals are phrases that indicate an agent has a concern
// worth investigating later.
var concernSignals = []string{
	"worried about",
	"should check",
	"need to verify",
	"might break",
	"concerned about",
	"want to confirm",
}

// ExtractConcerns pulls concern sentences from a transcript. Returns
// the sentences that contain concern signals.
func ExtractConcerns(transcript string) []string {
	sentences := splitSentences(transcript)
	var concerns []string

	for _, sentence := range sentences {
		lower := strings.ToLower(sentence)
		for _, signal := range concernSignals {
			if strings.Contains(lower, signal) {
				concerns = append(concerns, strings.TrimSpace(sentence))
				break
			}
		}
	}

	return concerns
}

// AddConcern stores a new concern for an agent.
func (tracker *ConcernTracker) AddConcern(role agent.Role, content, ticket string) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	tracker.concerns = append(tracker.concerns, AgentConcern{
		Role:      role,
		Content:   content,
		Ticket:    ticket,
		CreatedAt: time.Now(),
	})
}

// AddConcernsFromText extracts concern signals from text and stores them.
func (tracker *ConcernTracker) AddConcernsFromText(role agent.Role, text, ticket string) {
	extracted := ExtractConcerns(text)
	for _, concern := range extracted {
		tracker.AddConcern(role, concern, ticket)
	}
}

// UnresolvedForRole returns unresolved concerns for the given role,
// oldest first.
func (tracker *ConcernTracker) UnresolvedForRole(role agent.Role) []AgentConcern {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	var unresolved []AgentConcern
	for _, concern := range tracker.concerns {
		if concern.Role == role && !concern.Resolved {
			unresolved = append(unresolved, concern)
		}
	}

	return unresolved
}

// ResolveConcern marks the first unresolved concern matching the
// content as resolved.
func (tracker *ConcernTracker) ResolveConcern(role agent.Role, content string) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	for idx := range tracker.concerns {
		if tracker.concerns[idx].Role == role && tracker.concerns[idx].Content == content && !tracker.concerns[idx].Resolved {
			tracker.concerns[idx].Resolved = true
			return
		}
	}
}

// AllConcerns returns all stored concerns. Used in testing.
func (tracker *ConcernTracker) AllConcerns() []AgentConcern {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	result := make([]AgentConcern, len(tracker.concerns))
	copy(result, tracker.concerns)
	return result
}

// InvestigateConcerns checks if any idle agent has unresolved concerns
// and runs a DirectSession to investigate the top one.
func (orch *Orchestrator) InvestigateConcerns(ctx context.Context, idleRoles []agent.Role) {
	if orch.concerns == nil {
		return
	}

	for _, role := range idleRoles {
		orch.investigateTopConcern(ctx, role)
	}
}

func (orch *Orchestrator) investigateTopConcern(ctx context.Context, role agent.Role) {
	unresolved := orch.concerns.UnresolvedForRole(role)
	if len(unresolved) == 0 {
		return
	}

	concern := unresolved[0]
	agentInstance, ok := orch.agents[role]
	if !ok {
		return
	}

	prompt := fmt.Sprintf(
		"You previously noted: '%s'. Investigate it now — use gh commands to check the code. "+
			"Report what you found in 1-2 sentences.",
		concern.Content)

	result, err := agentInstance.DirectSession(ctx, prompt)
	if err != nil {
		log.Printf("concern investigation failed for %s: %v", role, err)
		return
	}

	response := filterPassResponse(result.Transcript)
	if response == "" {
		orch.concerns.ResolveConcern(role, concern.Content)
		return
	}

	log.Printf("concern: %s investigated: %s", role, concern.Content)
	orch.concerns.ResolveConcern(role, concern.Content)

	clean := cleanIdleResponse(response)
	if clean == "" {
		return
	}

	orch.postAsRole(ctx, "engineering", clean, role)
}

// SetConcernTracker connects the concern tracker to the orchestrator.
func (orch *Orchestrator) SetConcernTracker(tracker *ConcernTracker) {
	orch.concerns = tracker
}
