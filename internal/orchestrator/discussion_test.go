package orchestrator_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscussionPhase_PostsPlan(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	planRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"I'll create auth/middleware.go and add JWT validation."}` + "\n"),
	}

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)
	engAgent := buildAgent(t, planRunner, agent.RoleEngineer1, db)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleEngineer1: engAgent,
	}

	server := newTestSlackServer()
	t.Cleanup(server.Close)
	bot := newTestSlackBot(server.URL)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		agents, checkIns, bot, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	assignment := orchestrator.Assignment{
		Role: agent.RoleEngineer1, Ticket: "JAM-42", Description: "Add auth",
	}

	discussion := orch.RunDiscussionForTest(ctx, engAgent, assignment)

	// The engineer should have been asked for a plan.
	require.NotEmpty(t, planRunner.calls)
	assert.Contains(t, planRunner.calls[0].stdin, "JAM-42")

	// Discussion result is returned (may be empty if no conversation engine).
	_ = discussion
}

func TestDiscussionPhase_PassResponse_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	passRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"PASS"}` + "\n"),
	}

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)
	engAgent := buildAgent(t, passRunner, agent.RoleEngineer1, db)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent, agent.RoleEngineer1: engAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	assignment := orchestrator.Assignment{
		Role: agent.RoleEngineer1, Ticket: "JAM-42", Description: "test",
	}

	discussion := orch.RunDiscussionForTest(ctx, engAgent, assignment)
	assert.Empty(t, discussion)
}

func TestDiscussionPhase_Error_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	errRunner := &fakeProcessRunner{
		output: []byte(`{"type":"error","content":"fail"}` + "\n"),
		err:    assert.AnError,
	}

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)
	engAgent := buildAgent(t, errRunner, agent.RoleEngineer1, db)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent, agent.RoleEngineer1: engAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	assignment := orchestrator.Assignment{
		Role: agent.RoleEngineer1, Ticket: "JAM-42", Description: "test",
	}

	discussion := orch.RunDiscussionForTest(ctx, engAgent, assignment)
	assert.Empty(t, discussion)
}

func TestDiscussionPhase_WithConversation_CollectsMessages(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	planRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"I'll add JWT middleware to the auth package."}` + "\n"),
	}
	chatRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Sounds good, watch the token expiry edge case."}` + "\n"),
	}

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}

	agents[agent.RoleEngineer1] = buildAgent(t, planRunner, agent.RoleEngineer1, db)
	agents[agent.RolePM] = buildAgent(t, pmRunner, agent.RolePM, db)
	factStores[agent.RoleEngineer1] = memory.NewFactStore(db)
	factStores[agent.RolePM] = memory.NewFactStore(db)

	for _, role := range allRoles {
		if role == agent.RoleEngineer1 || role == agent.RolePM {
			continue
		}
		agents[role] = buildAgent(t, chatRunner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	server := newTestSlackServer()
	t.Cleanup(server.Close)
	bot := newTestSlackBot(server.URL)

	conversation := orchestrator.NewConversationEngine(agents, factStores, bot, nil)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		agents, checkIns, bot, orchestrator.NewAssigner(agents[agent.RolePM], "TEST"),
	)
	orch.SetConversationEngine(conversation)

	// Seed some recent messages so collectDiscussion returns content.
	conversation.OnMessage(ctx, "engineering", "ceo", "let's discuss the auth approach")

	assignment := orchestrator.Assignment{
		Role: agent.RoleEngineer1, Ticket: "JAM-42", Description: "Add JWT auth",
	}

	discussion := orch.RunDiscussionForTest(ctx, agents[agent.RoleEngineer1], assignment)

	assert.Contains(t, discussion, "Team Discussion")
}

func TestArchitectureReview_NoTechLead_ReturnsApproved(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(context.Background()))

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)

	// No tech lead in agents.
	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	outcome := orch.RunArchitectureReviewForTest(context.Background(), "https://github.com/test-org/test-repo/pull/1", "T-1")

	assert.Equal(t, orchestrator.ReviewApproved, outcome)
}

func TestArchitectureReview_WithTechLead_ReturnsOutcome(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	techLeadRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Architecture looks clean. APPROVED"}` + "\n"),
	}

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)
	techLead := buildAgent(t, techLeadRunner, agent.RoleTechLead, db)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent, agent.RoleTechLead: techLead},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	outcome := orch.RunArchitectureReviewForTest(ctx, "https://github.com/test-org/test-repo/pull/1", "T-1")

	assert.Equal(t, orchestrator.ReviewApproved, outcome)
	require.NotEmpty(t, techLeadRunner.calls)
	assert.Contains(t, techLeadRunner.calls[0].stdin, "gh pr diff 1")
}

func TestFilterPassResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty", "", ""},
		{"pass", "PASS", ""},
		{"contains pass", "I'll PASS on this", ""},
		{"real content", "I'll create auth/middleware.go", "I'll create auth/middleware.go"},
		{"whitespace", "  ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := orchestrator.FilterPassResponseForTest(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
