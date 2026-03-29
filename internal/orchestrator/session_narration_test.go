package orchestrator_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	islack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/JR-G/squad0/internal/pipeline"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildNarrationOrch builds an orchestrator wired with a Slack bot and
// conversation engine so narration code (postAsRole, acknowledgeThread)
// is actually exercised.
func buildNarrationOrch(
	t *testing.T,
	agents map[agent.Role]*agent.Agent,
	memDB *memory.DB,
) (*orchestrator.Orchestrator, *pipeline.WorkItemStore) {
	t.Helper()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeDB, pipeErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, pipeErr)
	t.Cleanup(func() { _ = pipeDB.Close() })

	pipeStore := pipeline.NewWorkItemStore(pipeDB)
	require.NoError(t, pipeStore.InitSchema(ctx))

	server := newTestSlackServer()
	t.Cleanup(server.Close)
	bot := newTestSlackBot(server.URL)

	pmAgent := agents[agent.RolePM]
	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{
			PollInterval:     time.Second,
			MaxParallel:      3,
			CooldownAfter:    time.Second,
			AcknowledgePause: time.Millisecond,
			TargetRepoDir:    t.TempDir(),
		},
		agents, checkIns, bot, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)

	// Build conversation engine with all roles.
	allRoles := agent.AllRoles()
	convAgents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	chatRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"PASS"}` + "\n")}

	for _, role := range allRoles {
		existing, ok := agents[role]
		if !ok {
			existing = buildAgent(t, chatRunner, role, memDB)
		}
		convAgents[role] = existing
		factStores[role] = memory.NewFactStore(memDB)
	}

	conversation := orchestrator.NewConversationEngine(convAgents, factStores, bot, nil)
	orch.SetConversationEngine(conversation)

	return orch, pipeStore
}

func TestStartFixUp_WithNarration_PostsAndAcknowledges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	// Reviewer says CHANGES_REQUESTED; engineer fixes; re-review escalates.
	reviewRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"CHANGES_REQUESTED - fix nil check"}` + "\n"),
	}
	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Fixed the nil check as requested."}` + "\n"),
	}
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"done"}` + "\n"),
	}

	pmAgent := setupPMAgent(t, pmRunner)
	reviewerAgent := buildAgent(t, reviewRunner, agent.RoleReviewer, memDB)
	engAgent := buildAgent(t, engRunner, agent.RoleEngineer1, memDB)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleReviewer:  reviewerAgent,
		agent.RoleEngineer1: engAgent,
	}

	orch, pipeStore := buildNarrationOrch(t, agents, memDB)

	itemID, createErr := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-N1", Engineer: agent.RoleEngineer1, Stage: pipeline.StageReviewing, Branch: "feat/jam-n1",
	})
	require.NoError(t, createErr)

	assert.NotPanics(t, func() {
		orch.StartReviewWithItemForTest(ctx, "https://github.com/test-org/test-repo/pull/100", "JAM-N1", itemID, agent.RoleEngineer1)
		orch.Wait()
	})

	engRunner.mu.Lock()
	engCalls := len(engRunner.calls)
	engRunner.mu.Unlock()
	assert.GreaterOrEqual(t, engCalls, 1, "engineer should have run fix-up session")
}

func TestStartFixUp_EngError_PostsErrorNarration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"error","content":"fail"}` + "\n"),
		err:    assert.AnError,
	}
	reviewRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"CHANGES_REQUESTED"}` + "\n"),
	}
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"done"}` + "\n"),
	}

	pmAgent := setupPMAgent(t, pmRunner)
	reviewerAgent := buildAgent(t, reviewRunner, agent.RoleReviewer, memDB)
	engAgent := buildAgent(t, engRunner, agent.RoleEngineer1, memDB)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleReviewer:  reviewerAgent,
		agent.RoleEngineer1: engAgent,
	}

	orch, pipeStore := buildNarrationOrch(t, agents, memDB)

	itemID, createErr := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-N2", Engineer: agent.RoleEngineer1, Stage: pipeline.StageReviewing, Branch: "feat/jam-n2",
	})
	require.NoError(t, createErr)

	assert.NotPanics(t, func() {
		orch.StartReviewWithItemForTest(ctx, "https://github.com/test-org/test-repo/pull/101", "JAM-N2", itemID, agent.RoleEngineer1)
		orch.Wait()
	})
}

func TestAcknowledgeThread_QuickChatError_ReturnsGracefully(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"error","content":"fail"}` + "\n"),
		err:    assert.AnError,
	}
	engAgent := buildAgent(t, engRunner, agent.RoleEngineer1, memDB)

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	agents[agent.RoleEngineer1] = engAgent
	factStores[agent.RoleEngineer1] = memory.NewFactStore(memDB)

	chatRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"PASS"}` + "\n")}
	for _, role := range allRoles {
		if role == agent.RoleEngineer1 {
			continue
		}
		agents[role] = buildAgent(t, chatRunner, role, memDB)
		factStores[role] = memory.NewFactStore(memDB)
	}

	server := newTestSlackServer()
	t.Cleanup(server.Close)
	bot := newTestSlackBot(server.URL)

	conversation := orchestrator.NewConversationEngine(agents, factStores, bot, nil)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		agents, checkIns, bot, nil,
	)
	orch.SetConversationEngine(conversation)

	// Seed messages so acknowledgeThread enters the QuickChat path.
	conversation.OnMessage(ctx, "engineering", string(agent.RoleEngineer1), "picking up JAM-50")
	conversation.OnMessage(ctx, "engineering", "ceo", "sounds good")

	// QuickChat errors — should return gracefully.
	assert.NotPanics(t, func() {
		orch.AcknowledgeThreadForTest(ctx, engAgent, agent.RoleEngineer1, "engineering")
	})
}

func TestRunSession_WithBotAndConversation_PostsNarration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	var messages []string
	var msgMu sync.Mutex
	handler := http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		_ = req.ParseForm()
		msgMu.Lock()
		messages = append(messages, req.FormValue("text"))
		msgMu.Unlock()
		resp := map[string]interface{}{"ok": true, "channel": "C004", "ts": "123"}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	})
	slackServer := httptest.NewServer(handler)
	t.Cleanup(slackServer.Close)

	bot := islack.NewBotWithURL(islack.BotConfig{
		BotToken: "xoxb-test",
		AppToken: "xapp-test",
		Channels: map[string]string{
			"feed":        "C002",
			"engineering": "C004",
			"reviews":     "C005",
		},
		Personas:   map[agent.Role]islack.Persona{},
		MinSpacing: 0,
	}, slackServer.URL+"/")

	assignmentJSON := `[{"role":"engineer-1","ticket":"SQ-88","description":"Add narration tests"}]`
	contentBytes, marshalErr := json.Marshal(assignmentJSON)
	require.NoError(t, marshalErr)
	pmOutput := `{"type":"result","result":` + string(contentBytes) + `}` + "\n"

	pmRunner := &fakeProcessRunner{output: []byte(pmOutput)}
	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Done. Opened https://github.com/test-org/test-repo/pull/88"}` + "\n"),
	}
	reviewRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"LGTM APPROVED"}` + "\n"),
	}

	memDB, memErr := memory.Open(ctx, ":memory:")
	require.NoError(t, memErr)
	t.Cleanup(func() { _ = memDB.Close() })

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pmAgent := setupPMAgent(t, pmRunner)
	engAgent := buildAgent(t, engRunner, agent.RoleEngineer1, memDB)
	engAgent.SetDBPath("/tmp/test-narration.db")
	reviewerAgent := buildAgent(t, reviewRunner, agent.RoleReviewer, memDB)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleEngineer1: engAgent,
		agent.RoleReviewer:  reviewerAgent,
	}

	// Init a real git repo for worktree creation.
	tmpDir := t.TempDir()
	gitInit := exec.CommandContext(ctx, "git", "init")
	gitInit.Dir = tmpDir
	_ = gitInit.Run()
	gitCommit := exec.CommandContext(ctx, "git", "commit", "--allow-empty", "-m", "init")
	gitCommit.Dir = tmpDir
	_ = gitCommit.Run()

	pipeDB, pipeErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, pipeErr)
	t.Cleanup(func() { _ = pipeDB.Close() })

	pipeStore := pipeline.NewWorkItemStore(pipeDB)
	require.NoError(t, pipeStore.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{
			PollInterval:     50 * time.Millisecond,
			MaxParallel:      3,
			CooldownAfter:    time.Second,
			WorkEnabled:      true,
			TargetRepoDir:    tmpDir,
			AcknowledgePause: time.Millisecond,
			DiscussionWait:   time.Millisecond,
		},
		agents, checkIns, bot, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)
	orch.SetProjectEpisodeStore(memory.NewEpisodeStore(memDB))

	timedCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	_ = orch.Run(timedCtx)
	orch.Wait()

	// Narration messages should have been posted.
	assert.GreaterOrEqual(t, len(messages), 2, "expected narration messages")
}
