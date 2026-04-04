package routing_test

import (
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/routing"
	"github.com/stretchr/testify/assert"
)

func TestTokenLedger_Record_TracksTicketSpend(t *testing.T) {
	t.Parallel()

	ledger := routing.NewTokenLedger(100000, 0)
	ledger.Record("JAM-1", agent.RoleEngineer1, 5000)
	ledger.Record("JAM-1", agent.RoleEngineer1, 3000)

	assert.Equal(t, int64(8000), ledger.TicketSpend("JAM-1"))
}

func TestTokenLedger_Record_TracksAgentDaily(t *testing.T) {
	t.Parallel()

	ledger := routing.NewTokenLedger(0, 200000)
	ledger.Record("JAM-1", agent.RoleEngineer1, 10000)
	ledger.Record("JAM-2", agent.RoleEngineer1, 15000)

	assert.Equal(t, int64(25000), ledger.AgentDailySpend(agent.RoleEngineer1))
}

func TestTokenLedger_IsTicketOverBudget(t *testing.T) {
	t.Parallel()

	ledger := routing.NewTokenLedger(10000, 0)
	ledger.Record("JAM-1", agent.RoleEngineer1, 11000)

	assert.True(t, ledger.IsTicketOverBudget("JAM-1"))
	assert.False(t, ledger.IsTicketOverBudget("JAM-2"))
}

func TestTokenLedger_IsTicketOverBudget_NoLimit(t *testing.T) {
	t.Parallel()

	ledger := routing.NewTokenLedger(0, 0)
	ledger.Record("JAM-1", agent.RoleEngineer1, 999999)

	assert.False(t, ledger.IsTicketOverBudget("JAM-1"))
}

func TestTokenLedger_IsAgentOverDailyBudget(t *testing.T) {
	t.Parallel()

	ledger := routing.NewTokenLedger(0, 50000)
	ledger.Record("JAM-1", agent.RoleEngineer2, 60000)

	assert.True(t, ledger.IsAgentOverDailyBudget(agent.RoleEngineer2))
	assert.False(t, ledger.IsAgentOverDailyBudget(agent.RoleEngineer1))
}

func TestTokenLedger_IsAgentOverDailyBudget_NoLimit(t *testing.T) {
	t.Parallel()

	ledger := routing.NewTokenLedger(0, 0)
	ledger.Record("JAM-1", agent.RoleEngineer1, 999999)

	assert.False(t, ledger.IsAgentOverDailyBudget(agent.RoleEngineer1))
}

func TestTokenLedger_EmptyTicket_DoesNotTrack(t *testing.T) {
	t.Parallel()

	ledger := routing.NewTokenLedger(10000, 0)
	ledger.Record("", agent.RoleEngineer1, 5000)

	assert.Equal(t, int64(0), ledger.TicketSpend(""))
}

func TestTokenLedger_SeparateTickets(t *testing.T) {
	t.Parallel()

	ledger := routing.NewTokenLedger(100000, 0)
	ledger.Record("JAM-1", agent.RoleEngineer1, 5000)
	ledger.Record("JAM-2", agent.RoleEngineer2, 3000)

	assert.Equal(t, int64(5000), ledger.TicketSpend("JAM-1"))
	assert.Equal(t, int64(3000), ledger.TicketSpend("JAM-2"))
}
