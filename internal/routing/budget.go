package routing

import (
	"fmt"
	"sync"
	"time"

	"github.com/JR-G/squad0/internal/agent"
)

// TokenLedger tracks token spend per ticket and per agent. Provides
// circuit breakers when spending exceeds configured limits.
type TokenLedger struct {
	mu             sync.Mutex
	ticketSpend    map[string]int64
	agentDaily     map[string]int64 // "role:YYYY-MM-DD" → tokens
	maxPerTicket   int64
	maxPerAgentDay int64
}

// NewTokenLedger creates a ledger with the given limits. Zero means
// no limit.
func NewTokenLedger(maxPerTicket, maxPerAgentDay int64) *TokenLedger {
	return &TokenLedger{
		ticketSpend:    make(map[string]int64),
		agentDaily:     make(map[string]int64),
		maxPerTicket:   maxPerTicket,
		maxPerAgentDay: maxPerAgentDay,
	}
}

// Record adds token usage for a ticket and agent.
func (ledger *TokenLedger) Record(ticket string, role agent.Role, tokens int64) {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()

	if ticket != "" {
		ledger.ticketSpend[ticket] += tokens
	}

	dailyKey := fmt.Sprintf("%s:%s", role, time.Now().Format("2006-01-02"))
	ledger.agentDaily[dailyKey] += tokens
}

// IsTicketOverBudget returns true if the ticket has exceeded its
// token budget.
func (ledger *TokenLedger) IsTicketOverBudget(ticket string) bool {
	if ledger.maxPerTicket <= 0 {
		return false
	}

	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	return ledger.ticketSpend[ticket] >= ledger.maxPerTicket
}

// IsAgentOverDailyBudget returns true if the agent has exceeded its
// daily token budget.
func (ledger *TokenLedger) IsAgentOverDailyBudget(role agent.Role) bool {
	if ledger.maxPerAgentDay <= 0 {
		return false
	}

	ledger.mu.Lock()
	defer ledger.mu.Unlock()

	dailyKey := fmt.Sprintf("%s:%s", role, time.Now().Format("2006-01-02"))
	return ledger.agentDaily[dailyKey] >= ledger.maxPerAgentDay
}

// TicketSpend returns total tokens spent on a ticket.
func (ledger *TokenLedger) TicketSpend(ticket string) int64 {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	return ledger.ticketSpend[ticket]
}

// AgentDailySpend returns tokens spent by an agent today.
func (ledger *TokenLedger) AgentDailySpend(role agent.Role) int64 {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()

	dailyKey := fmt.Sprintf("%s:%s", role, time.Now().Format("2006-01-02"))
	return ledger.agentDaily[dailyKey]
}
