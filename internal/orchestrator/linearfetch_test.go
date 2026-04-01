package orchestrator_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLinearResponse_ValidJSON(t *testing.T) {
	t.Parallel()

	body := `{"data":{"team":{"issues":{"nodes":[
		{"identifier":"JAM-1","title":"Auth","description":"Depends on: JAM-0","priority":1,"state":{"name":"Todo"},"labels":{"nodes":[{"name":"API"}]}},
		{"identifier":"JAM-2","title":"UI","description":"No deps","priority":3,"state":{"name":"Backlog"},"labels":{"nodes":[]}}
	]}}}}`

	tickets, err := orchestrator.ParseLinearResponseForTest([]byte(body))
	require.NoError(t, err)
	require.Len(t, tickets, 2)

	assert.Equal(t, "JAM-1", tickets[0].ID)
	assert.Equal(t, "Auth", tickets[0].Title)
	assert.Equal(t, 1, tickets[0].Priority)
	assert.Equal(t, []string{"API"}, tickets[0].Labels)
	assert.Equal(t, []string{"JAM-0"}, tickets[0].DependsOn)
	assert.Equal(t, "Todo", tickets[0].State)

	assert.Equal(t, "JAM-2", tickets[1].ID)
	assert.Empty(t, tickets[1].DependsOn)
}

func TestParseLinearResponse_EmptyNodes(t *testing.T) {
	t.Parallel()

	body := `{"data":{"team":{"issues":{"nodes":[]}}}}`
	tickets, err := orchestrator.ParseLinearResponseForTest([]byte(body))
	require.NoError(t, err)
	assert.Empty(t, tickets)
}

func TestParseLinearResponse_InvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := orchestrator.ParseLinearResponseForTest([]byte("not json"))
	assert.Error(t, err)
}

func TestFetchLinearTickets_InvalidURL_ReturnsError(t *testing.T) {
	t.Parallel()

	_, err := orchestrator.FetchLinearTickets(context.Background(), "key", "TEAM")
	// Will fail because the real Linear API won't accept "key" as auth.
	// But the function should return an error, not panic.
	assert.Error(t, err)
}

func TestFetchLinearTickets_MockServer(t *testing.T) {
	t.Parallel()

	body := `{"data":{"team":{"issues":{"nodes":[
		{"identifier":"JAM-10","title":"Test ticket","description":"","priority":2,"state":{"name":"Todo"},"labels":{"nodes":[{"name":"API"}]}}
	]}}}}`

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "test-key", req.Header.Get("Authorization"))
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	tickets, err := orchestrator.FetchLinearTicketsWithURL(context.Background(), "test-key", "TEAM-1", server.URL)
	require.NoError(t, err)
	require.Len(t, tickets, 1)
	assert.Equal(t, "JAM-10", tickets[0].ID)
	assert.Equal(t, []string{"API"}, tickets[0].Labels)
}

func TestFetchLinearTickets_ServerError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte("internal error"))
	}))
	t.Cleanup(server.Close)

	_, err := orchestrator.FetchLinearTicketsWithURL(context.Background(), "key", "TEAM", server.URL)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestParseLinearResponse_Roundtrip(t *testing.T) {
	t.Parallel()

	// Simulate what Linear actually returns.
	type node struct {
		Identifier  string `json:"identifier"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Priority    int    `json:"priority"`
		State       struct {
			Name string `json:"name"`
		} `json:"state"`
		Labels struct {
			Nodes []struct {
				Name string `json:"name"`
			} `json:"nodes"`
		} `json:"labels"`
	}

	testNode := node{Identifier: "JAM-99", Title: "Test", Description: "**Depends on:** JAM-7, JAM-8", Priority: 2}
	testNode.State.Name = "Todo"
	testNode.Labels.Nodes = append(testNode.Labels.Nodes, struct {
		Name string `json:"name"`
	}{"Frontend"}, struct {
		Name string `json:"name"`
	}{"API"})

	wrapper := map[string]any{
		"data": map[string]any{
			"team": map[string]any{
				"issues": map[string]any{
					"nodes": []node{testNode},
				},
			},
		},
	}

	body, err := json.Marshal(wrapper)
	require.NoError(t, err)

	tickets, parseErr := orchestrator.ParseLinearResponseForTest(body)
	require.NoError(t, parseErr)
	require.Len(t, tickets, 1)

	assert.Equal(t, "JAM-99", tickets[0].ID)
	assert.Equal(t, []string{"Frontend", "API"}, tickets[0].Labels)
	assert.Equal(t, []string{"JAM-7", "JAM-8"}, tickets[0].DependsOn)
}
