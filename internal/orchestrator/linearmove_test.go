package orchestrator_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMoveLinearTicketStateAPI_Success_MovesTicket(t *testing.T) {
	t.Parallel()

	var capturedMutation string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		text := string(body)

		switch {
		case strings.Contains(text, "states { nodes"):
			_, _ = w.Write([]byte(`{"data":{"team":{"states":{"nodes":[{"id":"state-done-id","name":"Done"},{"id":"state-review-id","name":"In Review"}]}}}}`))
		case strings.Contains(text, "issueUpdate"):
			capturedMutation = text
			_, _ = w.Write([]byte(`{"data":{"issueUpdate":{"success":true,"issue":{"state":{"name":"Done"}}}}}`))
		default:
			http.Error(w, "unexpected query", http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)

	err := orchestrator.MoveLinearTicketStateAPIWithURL(
		context.Background(), "test-key", "team-1", "JAM-42", "Done", server.URL,
	)
	require.NoError(t, err)
	assert.Contains(t, capturedMutation, "JAM-42")
	assert.Contains(t, capturedMutation, "state-done-id")
}

func TestMoveLinearTicketStateAPI_UnknownState_ReturnsError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"team":{"states":{"nodes":[{"id":"state-done-id","name":"Done"}]}}}}`))
	}))
	t.Cleanup(server.Close)

	err := orchestrator.MoveLinearTicketStateAPIWithURL(
		context.Background(), "test-key", "team-1", "JAM-42", "Nonexistent", server.URL,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Nonexistent")
}

func TestMoveLinearTicketStateAPI_IssueUpdateError_ReturnsError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		text := string(body)
		if strings.Contains(text, "states { nodes") {
			_, _ = w.Write([]byte(`{"data":{"team":{"states":{"nodes":[{"id":"s1","name":"Done"}]}}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"errors":[{"message":"issue not found"}]}`))
	}))
	t.Cleanup(server.Close)

	err := orchestrator.MoveLinearTicketStateAPIWithURL(
		context.Background(), "key", "team-1", "JAM-NOPE", "Done", server.URL,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "issue not found")
}

func TestMoveLinearTicketStateAPI_NonOKStatus_ReturnsError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorised", http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	err := orchestrator.MoveLinearTicketStateAPIWithURL(
		context.Background(), "bad-key", "team-1", "JAM-42", "Done", server.URL,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestMoveLinearTicketStateAPI_MissingArgs_ReturnsError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		apiKey  string
		teamID  string
		ticket  string
		target  string
		wantMsg string
	}{
		{"no api key", "", "team", "JAM-1", "Done", "API key"},
		{"no team", "k", "", "JAM-1", "Done", "team"},
		{"no ticket", "k", "team", "", "Done", "ticket and target"},
		{"no target", "k", "team", "JAM-1", "", "ticket and target"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := orchestrator.MoveLinearTicketStateAPIWithURL(
				context.Background(), tc.apiKey, tc.teamID, tc.ticket, tc.target, "http://localhost",
			)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantMsg)
		})
	}
}

func TestMoveLinearTicketStateAPI_MalformedStatesResponse_ReturnsError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	t.Cleanup(server.Close)

	err := orchestrator.MoveLinearTicketStateAPIWithURL(
		context.Background(), "key", "team-1", "JAM-1", "Done", server.URL,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing states")
}

func TestMoveLinearTicketStateAPI_MalformedMutationResponse_ReturnsError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "states { nodes") {
			_, _ = w.Write([]byte(`{"data":{"team":{"states":{"nodes":[{"id":"s","name":"Done"}]}}}}`))
			return
		}
		_, _ = w.Write([]byte(`garbled`))
	}))
	t.Cleanup(server.Close)

	err := orchestrator.MoveLinearTicketStateAPIWithURL(
		context.Background(), "key", "team-1", "JAM-1", "Done", server.URL,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "issueUpdate")
}

func TestMoveLinearTicketStateAPI_SuccessFalse_ReturnsError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "states { nodes") {
			_, _ = w.Write([]byte(`{"data":{"team":{"states":{"nodes":[{"id":"s","name":"Done"}]}}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"issueUpdate":{"success":false}}}`))
	}))
	t.Cleanup(server.Close)

	err := orchestrator.MoveLinearTicketStateAPIWithURL(
		context.Background(), "key", "team-1", "JAM-1", "Done", server.URL,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "success=false")
}

func TestMoveLinearTicketStateAPI_DefaultURL_UsesLiveEndpoint(t *testing.T) {
	t.Parallel()
	// With a bogus key + short timeout, the call fails networkly but
	// exercises the default-URL path.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediate cancel so we don't actually hit the live API

	err := orchestrator.MoveLinearTicketStateAPI(ctx, "bad-key", "team-1", "JAM-1", "Done")
	require.Error(t, err)
}

func TestMoveLinearTicketStateAPI_InvalidURL_ReturnsRequestError(t *testing.T) {
	t.Parallel()

	// Whitespace inside scheme triggers http.NewRequestWithContext to
	// fail before any network call — covers the request-creation
	// error branch of linearPost.
	err := orchestrator.MoveLinearTicketStateAPIWithURL(
		context.Background(), "key", "team-1", "JAM-1", "Done", "http://exa\nmple.com",
	)
	require.Error(t, err)
}
