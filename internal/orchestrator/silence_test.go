package orchestrator_test

import (
	"context"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBusyChecker_ExcludesBusyAgent_FromChat(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	// All engineers emit PASS so any response is observable via call counts.
	eng1Runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"engineer-1 says hi"}` + "\n")}
	eng2Runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"engineer-2 says hi"}` + "\n")}
	eng3Runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"engineer-3 says hi"}` + "\n")}
	otherRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"PASS"}` + "\n")}

	agents := map[agent.Role]*agent.Agent{
		agent.RoleEngineer1: buildAgent(t, eng1Runner, agent.RoleEngineer1, memDB),
		agent.RoleEngineer2: buildAgent(t, eng2Runner, agent.RoleEngineer2, memDB),
		agent.RoleEngineer3: buildAgent(t, eng3Runner, agent.RoleEngineer3, memDB),
	}
	factStores := make(map[agent.Role]*memory.FactStore)
	for _, role := range agent.AllRoles() {
		if _, ok := agents[role]; !ok {
			agents[role] = buildAgent(t, otherRunner, role, memDB)
		}
		factStores[role] = memory.NewFactStore(memDB)
	}

	roster := map[agent.Role]string{
		agent.RoleEngineer1: "Callum",
		agent.RoleEngineer2: "Mara",
		agent.RoleEngineer3: "Cormac",
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, roster)

	// Engineer-1 is heads-down and should never be picked.
	engine.SetBusyChecker(func(_ context.Context, role agent.Role) bool {
		return role == agent.RoleEngineer1
	})

	// Human sends a message so responders get picked.
	engine.OnMessage(ctx, "engineering", "ceo", "what should we do?")

	// Wait briefly for the goroutines.
	time.Sleep(50 * time.Millisecond)

	eng1Runner.mu.Lock()
	eng1Calls := len(eng1Runner.calls)
	eng1Runner.mu.Unlock()
	assert.Equal(t, 0, eng1Calls, "busy engineer-1 should never be picked to respond")
}

func TestBusyChecker_MentionedBusyAgent_StaysSilent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	eng1Runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"engineer-1 replies"}` + "\n")}
	otherRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"PASS"}` + "\n")}

	agents := make(map[agent.Role]*agent.Agent, len(agent.AllRoles()))
	factStores := make(map[agent.Role]*memory.FactStore)
	runnerFor := func(role agent.Role) *fakeProcessRunner {
		if role == agent.RoleEngineer1 {
			return eng1Runner
		}
		return otherRunner
	}
	for _, role := range agent.AllRoles() {
		agents[role] = buildAgent(t, runnerFor(role), role, memDB)
		factStores[role] = memory.NewFactStore(memDB)
	}

	roster := map[agent.Role]string{
		agent.RoleEngineer1: "Callum",
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, roster)
	engine.SetBusyChecker(func(_ context.Context, role agent.Role) bool {
		return role == agent.RoleEngineer1
	})

	// Callum is mentioned but busy — must stay silent.
	engine.OnMessage(ctx, "engineering", "ceo", "Callum, thoughts on this?")
	time.Sleep(50 * time.Millisecond)

	eng1Runner.mu.Lock()
	eng1Calls := len(eng1Runner.calls)
	eng1Runner.mu.Unlock()
	assert.Equal(t, 0, eng1Calls, "mentioned but busy engineer-1 should stay silent — heads-down policy")
}

func TestBusyChecker_NotSet_BehaviourUnchanged(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	allRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"hi"}` + "\n")}

	agents := make(map[agent.Role]*agent.Agent, len(agent.AllRoles()))
	factStores := make(map[agent.Role]*memory.FactStore)
	for _, role := range agent.AllRoles() {
		agents[role] = buildAgent(t, allRunner, role, memDB)
		factStores[role] = memory.NewFactStore(memDB)
	}

	roster := map[agent.Role]string{agent.RoleEngineer1: "Callum"}
	engine := orchestrator.NewConversationEngine(agents, factStores, nil, roster)

	// No busy checker set — engine must not panic.
	assert.NotPanics(t, func() {
		engine.OnMessage(ctx, "engineering", "ceo", "anyone?")
	})
}
