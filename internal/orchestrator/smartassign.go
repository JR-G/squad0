package orchestrator

import (
	"context"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/pipeline"
)

// SmartAssigner handles ticket-to-engineer matching using dependency
// checking, priority ordering, skill matching, and circuit breakers.
// The PM agent is only consulted for genuine tiebreakers.
type SmartAssigner struct {
	mu              sync.Mutex
	failureCounts   map[string]int       // ticket → failure count
	deferredTickets map[string]time.Time // ticket → deferred until
	pipelineStore   *pipeline.WorkItemStore
	doneTickets     map[string]bool // cache of completed ticket IDs
	maxFailures     int
}

// NewSmartAssigner creates a SmartAssigner with default settings.
func NewSmartAssigner(pipelineStore *pipeline.WorkItemStore) *SmartAssigner {
	return &SmartAssigner{
		failureCounts:   make(map[string]int),
		deferredTickets: make(map[string]time.Time),
		pipelineStore:   pipelineStore,
		doneTickets:     make(map[string]bool),
		maxFailures:     3,
	}
}

// RecordFailure increments the failure count for a ticket.
func (sa *SmartAssigner) RecordFailure(ticket string) int {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	sa.failureCounts[ticket]++
	return sa.failureCounts[ticket]
}

// FailureCount returns the current failure count for a ticket.
func (sa *SmartAssigner) FailureCount(ticket string) int {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	return sa.failureCounts[ticket]
}

// IsCircuitOpen returns true if the ticket has failed too many times.
func (sa *SmartAssigner) IsCircuitOpen(ticket string) bool {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	return sa.failureCounts[ticket] >= sa.maxFailures
}

// DeferTicket marks a ticket as deferred for the given duration.
// The assigner will skip this ticket until the deferral expires.
func (sa *SmartAssigner) DeferTicket(ticket string, duration time.Duration) {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	sa.deferredTickets[ticket] = time.Now().Add(duration)
	log.Printf("assign: deferred %s for %s", ticket, duration)
}

// IsDeferred returns true if the ticket is currently deferred.
func (sa *SmartAssigner) IsDeferred(ticket string) bool {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	until, ok := sa.deferredTickets[ticket]
	if !ok {
		return false
	}
	if time.Now().After(until) {
		delete(sa.deferredTickets, ticket)
		return false
	}
	return true
}

// FilterAndRank takes raw Linear tickets and idle engineers, and
// returns assignments sorted by priority with dependency checking,
// circuit breakers, and skill matching applied.
func (sa *SmartAssigner) FilterAndRank(ctx context.Context, tickets []LinearTicket, idleEngineers []agent.Role) []Assignment {
	sa.refreshDoneCache(ctx)

	// Filter out tickets that can't be worked.
	eligible := sa.filterEligible(ctx, tickets)

	// Sort by priority (Urgent=1 first, Low=4 last, None=0 last).
	sort.Slice(eligible, func(i, j int) bool {
		return priorityRank(eligible[i].Priority) < priorityRank(eligible[j].Priority)
	})

	// Match tickets to engineers by skill.
	return sa.matchToEngineers(eligible, idleEngineers)
}

func (sa *SmartAssigner) filterEligible(ctx context.Context, tickets []LinearTicket) []LinearTicket {
	eligible := make([]LinearTicket, 0, len(tickets))

	for _, ticket := range tickets {
		if sa.IsDeferred(ticket.ID) {
			log.Printf("assign: skipping %s — deferred by PM", ticket.ID)
			continue
		}

		if sa.IsCircuitOpen(ticket.ID) {
			log.Printf("assign: skipping %s — circuit open (%d failures)", ticket.ID, sa.failureCounts[ticket.ID])
			continue
		}

		if !sa.dependenciesMet(ticket) {
			log.Printf("assign: skipping %s — unmet deps: %v", ticket.ID, ticket.DependsOn)
			continue
		}

		if sa.isAlreadyInPipeline(ctx, ticket.ID) {
			continue
		}

		eligible = append(eligible, ticket)
	}

	return eligible
}

func (sa *SmartAssigner) dependenciesMet(ticket LinearTicket) bool {
	for _, dep := range ticket.DependsOn {
		if !sa.doneTickets[dep] {
			return false
		}
	}
	return true
}

func (sa *SmartAssigner) isAlreadyInPipeline(ctx context.Context, ticket string) bool {
	if sa.pipelineStore == nil {
		return false
	}

	items, err := sa.pipelineStore.GetByTicket(ctx, ticket)
	if err != nil {
		return false
	}

	for _, item := range items {
		if !item.Stage.IsTerminal() {
			return true
		}
		// A recently failed item with a PR means the engineer went
		// idle while the PR was in review. Only block reassignment
		// for recent failures — old ones are stale.
		if item.Stage == pipeline.StageFailed && item.PRURL != "" && time.Since(item.UpdatedAt) < 2*time.Hour {
			log.Printf("assign: skipping %s — recently failed with open PR (%s)", ticket, item.PRURL)
			return true
		}
	}

	return false
}

func (sa *SmartAssigner) refreshDoneCache(ctx context.Context) {
	if sa.pipelineStore == nil {
		return
	}

	done, err := sa.pipelineStore.CompletedTickets(ctx)
	if err != nil {
		return
	}

	sa.mu.Lock()
	defer sa.mu.Unlock()
	for _, ticket := range done {
		sa.doneTickets[ticket] = true
	}
}

func (sa *SmartAssigner) matchToEngineers(tickets []LinearTicket, engineers []agent.Role) []Assignment {
	assignments := make([]Assignment, 0, len(engineers))
	used := make(map[agent.Role]bool)
	assigned := make(map[string]bool)

	// First pass: match by skill.
	for _, ticket := range tickets {
		if len(assignments) >= len(engineers) {
			break
		}

		best := bestEngineerForTicket(ticket, engineers, used)
		if best == "" {
			continue
		}

		used[best] = true
		assigned[ticket.ID] = true
		assignments = append(assignments, Assignment{
			Role:        best,
			Ticket:      ticket.ID,
			Description: ticket.Title + "\n\n" + truncateDescription(ticket.Description, 2000),
		})
	}

	return assignments
}

func bestEngineerForTicket(ticket LinearTicket, engineers []agent.Role, used map[agent.Role]bool) agent.Role {
	// Score each engineer for this ticket.
	var bestRole agent.Role
	bestScore := -1

	for _, role := range engineers {
		if used[role] {
			continue
		}

		score := skillScore(role, ticket.Labels)
		if score > bestScore {
			bestScore = score
			bestRole = role
		}
	}

	return bestRole
}

func skillScore(role agent.Role, labels []string) int {
	score := 1 // base score — everyone can work on anything

	for _, label := range labels {
		lower := strings.ToLower(label)
		score += roleMatchScore(role, lower)
	}

	return score
}

func roleMatchScore(role agent.Role, label string) int {
	switch role { //nolint:exhaustive // only engineers are skill-matched
	case agent.RoleEngineer1: // backend-leaning
		if strings.Contains(label, "api") || strings.Contains(label, "backend") {
			return 3
		}
	case agent.RoleEngineer2: // frontend-leaning
		if strings.Contains(label, "frontend") || strings.Contains(label, "ui") {
			return 3
		}
	case agent.RoleEngineer3: // infra/DX
		if strings.Contains(label, "deploy") || strings.Contains(label, "infra") || strings.Contains(label, "build") {
			return 3
		}
	}
	return 0
}

func priorityRank(priority int) int {
	// Linear: 1=Urgent, 2=High, 3=Normal, 4=Low, 0=None
	// We want Urgent first, None last.
	if priority == 0 {
		return 5
	}
	return priority
}

func truncateDescription(desc string, maxLen int) string {
	if len(desc) <= maxLen {
		return desc
	}
	return desc[:maxLen]
}
