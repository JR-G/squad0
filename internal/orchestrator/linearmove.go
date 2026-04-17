package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// MoveLinearTicketStateAPI transitions a Linear ticket to the given
// workflow-state name by calling the GraphQL API directly. No Claude
// session, no MCP — deterministic and fast.
//
// Two calls: one to look up the workflow-state ID by team + name, one
// to issueUpdate. Linear's identifier-based issueUpdate works with
// either the UUID or the "JAM-12" identifier.
func MoveLinearTicketStateAPI(ctx context.Context, apiKey, teamID, ticket, targetState string) error {
	return moveLinearTicketStateAPI(ctx, apiKey, teamID, ticket, targetState, "https://api.linear.app/graphql")
}

// MoveLinearTicketStateAPIWithURL allows overriding the API URL for
// testing.
func MoveLinearTicketStateAPIWithURL(ctx context.Context, apiKey, teamID, ticket, targetState, apiURL string) error {
	return moveLinearTicketStateAPI(ctx, apiKey, teamID, ticket, targetState, apiURL)
}

func moveLinearTicketStateAPI(ctx context.Context, apiKey, teamID, ticket, targetState, apiURL string) error {
	if apiKey == "" {
		return fmt.Errorf("linear API key not configured")
	}
	if teamID == "" {
		return fmt.Errorf("linear team ID not configured")
	}
	if ticket == "" || targetState == "" {
		return fmt.Errorf("ticket and target state are required")
	}

	stateID, err := lookupWorkflowStateID(ctx, apiKey, teamID, targetState, apiURL)
	if err != nil {
		return fmt.Errorf("resolving state %q: %w", targetState, err)
	}

	return issueUpdateStateID(ctx, apiKey, ticket, stateID, apiURL)
}

func lookupWorkflowStateID(ctx context.Context, apiKey, teamID, targetState, apiURL string) (string, error) {
	query := fmt.Sprintf(`{"query": "{ team(id:\"%s\") { states { nodes { id name } } } }"}`, teamID)
	body, err := linearPost(ctx, apiKey, apiURL, query)
	if err != nil {
		return "", err
	}

	var resp struct {
		Data struct {
			Team struct {
				States struct {
					Nodes []struct {
						ID   string `json:"id"`
						Name string `json:"name"`
					} `json:"nodes"`
				} `json:"states"`
			} `json:"team"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parsing states response: %w", err)
	}

	for _, state := range resp.Data.Team.States.Nodes {
		if strings.EqualFold(state.Name, targetState) {
			return state.ID, nil
		}
	}

	return "", fmt.Errorf("no workflow state named %q on team %s", targetState, teamID)
}

func issueUpdateStateID(ctx context.Context, apiKey, ticket, stateID, apiURL string) error {
	mutation := fmt.Sprintf(
		`{"query": "mutation { issueUpdate(id:\"%s\", input:{stateId:\"%s\"}) { success issue { state { name } } } }"}`,
		ticket, stateID,
	)
	body, err := linearPost(ctx, apiKey, apiURL, mutation)
	if err != nil {
		return err
	}

	var resp struct {
		Data struct {
			IssueUpdate struct {
				Success bool `json:"success"`
				Issue   struct {
					State struct {
						Name string `json:"name"`
					} `json:"state"`
				} `json:"issue"`
			} `json:"issueUpdate"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("parsing issueUpdate response: %w", err)
	}

	if len(resp.Errors) > 0 {
		return fmt.Errorf("linear returned error: %s", resp.Errors[0].Message)
	}

	if !resp.Data.IssueUpdate.Success {
		return fmt.Errorf("linear issueUpdate returned success=false")
	}

	return nil
}

func linearPost(ctx context.Context, apiKey, apiURL, body string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBufferString(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http call: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("linear returned %d: %s", resp.StatusCode, string(out))
	}

	return out, nil
}
