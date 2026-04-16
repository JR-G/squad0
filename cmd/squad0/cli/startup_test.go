package cli_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JR-G/squad0/cmd/squad0/cli"
	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/config"
	islack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSeedConversationHistory_NilBot_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	agents := make(map[agent.Role]*agent.Agent)
	factStores := make(map[agent.Role]*memory.FactStore)
	conversation := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	assert.NotPanics(t, func() {
		cli.SeedConversationHistory(ctx, nil, conversation, config.DefaultConfig())
	})
}

func TestSeedConversationHistory_NilConversation_DoesNotPanic(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{"ok": true, "messages": []map[string]string{}}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	}))
	t.Cleanup(server.Close)

	bot := islack.NewBotWithURL(islack.BotConfig{
		BotToken: "xoxb-test",
		AppToken: "xapp-test",
		Channels: map[string]string{
			"engineering": "C001",
			"reviews":     "C002",
			"feed":        "C003",
		},
	}, server.URL+"/")

	assert.NotPanics(t, func() {
		cli.SeedConversationHistory(context.Background(), bot, nil, config.DefaultConfig())
	})
}

func TestSeedConversationHistory_LoadRecentMessagesFails_ContinuesToNextChannel(t *testing.T) {
	t.Parallel()

	// Return an error for every channel request.
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{
			"ok":    false,
			"error": "channel_not_found",
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	}))
	t.Cleanup(server.Close)

	bot := islack.NewBotWithURL(islack.BotConfig{
		BotToken: "xoxb-test",
		AppToken: "xapp-test",
		Channels: map[string]string{
			"engineering": "C001",
			"reviews":     "C002",
			"feed":        "C003",
		},
	}, server.URL+"/")

	agents := make(map[agent.Role]*agent.Agent)
	factStores := make(map[agent.Role]*memory.FactStore)
	conversation := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	// Should not panic even when all channels fail to load.
	assert.NotPanics(t, func() {
		cli.SeedConversationHistory(context.Background(), bot, conversation, config.DefaultConfig())
	})
}

func TestSeedConversationHistory_SuccessfulLoad_SeedsMessages(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		resp := map[string]interface{}{
			"ok": true,
			"messages": []map[string]string{
				{"user": "U002", "text": "recent discussion", "ts": "2.0"},
				{"user": "U001", "text": "earlier discussion", "ts": "1.0"},
			},
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(resp)
	}))
	t.Cleanup(server.Close)

	bot := islack.NewBotWithURL(islack.BotConfig{
		BotToken: "xoxb-test",
		AppToken: "xapp-test",
		Channels: map[string]string{
			"engineering": "C001",
			"reviews":     "C002",
			"feed":        "C003",
		},
	}, server.URL+"/")

	agents := make(map[agent.Role]*agent.Agent)
	factStores := make(map[agent.Role]*memory.FactStore)
	conversation := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	// Should complete without error.
	assert.NotPanics(t, func() {
		cli.SeedConversationHistory(context.Background(), bot, conversation, config.DefaultConfig())
	})
}

func TestSeedConversationHistory_BothNil_DoesNotPanic(t *testing.T) {
	t.Parallel()

	assert.NotPanics(t, func() {
		cli.SeedConversationHistory(context.Background(), nil, nil, config.DefaultConfig())
	})
}

func TestAssertMemoryStoresWired_AllWired_ReturnsNil(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	embedder := memory.NewEmbedder("http://localhost:1", "test")
	episodeStore := memory.NewEpisodeStore(db)
	wired := agent.NewAgent(agent.RoleEngineer1, "test-model", nil, nil, nil, db, episodeStore, embedder)
	wired.SetMemoryStores(memory.NewGraphStore(db), memory.NewFactStore(db))

	agents := map[agent.Role]*agent.Agent{agent.RoleEngineer1: wired}

	assert.NoError(t, cli.AssertMemoryStoresWired(agents))
}

func TestAssertMemoryStoresWired_MissingStores_ReturnsError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	embedder := memory.NewEmbedder("http://localhost:1", "test")
	episodeStore := memory.NewEpisodeStore(db)
	missing := agent.NewAgent(agent.RoleEngineer2, "test-model", nil, nil, nil, db, episodeStore, embedder)
	// Deliberately do NOT call SetMemoryStores.

	agents := map[agent.Role]*agent.Agent{agent.RoleEngineer2: missing}

	gotErr := cli.AssertMemoryStoresWired(agents)
	require.Error(t, gotErr)
	assert.Contains(t, gotErr.Error(), "engineer-2")
}

func TestAssertMemoryStoresWired_NilAgent_ReturnsError(t *testing.T) {
	t.Parallel()

	agents := map[agent.Role]*agent.Agent{agent.RolePM: nil}

	gotErr := cli.AssertMemoryStoresWired(agents)
	require.Error(t, gotErr)
	assert.Contains(t, gotErr.Error(), "pm")
}
