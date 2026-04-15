package orchestrator

import (
	"context"

	"github.com/JR-G/squad0/internal/agent"
)

// BusyChecker returns true if the given role is currently in a
// heads-down work session and should be excluded from chat responses.
// Lets the conversation engine stay silent around an engineer who is
// implementing a ticket so they're not pulled back into Slack spirals.
type BusyChecker func(ctx context.Context, role agent.Role) bool

// SetBusyChecker registers a function that the engine calls before
// picking an agent as a chat responder. Busy agents (engineers in a
// heads-down work session) are silently skipped so the conversation
// engine doesn't interrupt active implementation work.
func (engine *ConversationEngine) SetBusyChecker(checker BusyChecker) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.busyChecker = checker
}

// isBusy reports whether the given role is currently in a heads-down
// work session and should be excluded from chat responses.
func (engine *ConversationEngine) isBusy(ctx context.Context, role agent.Role) bool {
	engine.mu.Lock()
	checker := engine.busyChecker
	engine.mu.Unlock()
	if checker == nil {
		return false
	}
	return checker(ctx, role)
}

// eligibleRoles returns the roles that can be picked as chat
// responders for the given sender, excluding the sender, the
// Reviewer, any role already in mentioned, and any role flagged as
// busy by the BusyChecker.
func (engine *ConversationEngine) eligibleRoles(ctx context.Context, sender string, mentioned []agent.Role) []agent.Role {
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
		if engine.isBusy(ctx, role) {
			continue
		}
		eligible = append(eligible, role)
	}

	return eligible
}

// filterBusy returns the subset of roles that are not currently in
// a heads-down work session. Mentioned agents are normally
// guaranteed a slot, except those who are busy — they stay silent.
func (engine *ConversationEngine) filterBusy(ctx context.Context, roles []agent.Role) []agent.Role {
	active := make([]agent.Role, 0, len(roles))
	for _, role := range roles {
		if engine.isBusy(ctx, role) {
			continue
		}
		active = append(active, role)
	}
	return active
}
