package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
)

// LinearTicket is a ticket fetched directly from the Linear API.
type LinearTicket struct {
	ID          string // e.g. "JAM-12"
	Title       string
	Description string
	Priority    int // 0=None, 1=Urgent, 2=High, 3=Normal, 4=Low
	Labels      []string
	State       string
	DependsOn   []string // parsed from description
}

// FetchLinearTickets queries the Linear GraphQL API directly for
// tickets in "Todo" or "Backlog" state. No AI session needed.
func FetchLinearTickets(ctx context.Context, apiKey, teamID string) ([]LinearTicket, error) {
	return fetchLinearTicketsFromURL(ctx, apiKey, teamID, "https://api.linear.app/graphql")
}

// FetchLinearTicketsWithURL allows overriding the API URL for testing.
func FetchLinearTicketsWithURL(ctx context.Context, apiKey, teamID, apiURL string) ([]LinearTicket, error) {
	return fetchLinearTicketsFromURL(ctx, apiKey, teamID, apiURL)
}

func fetchLinearTicketsFromURL(ctx context.Context, apiKey, teamID, apiURL string) ([]LinearTicket, error) {
	query := fmt.Sprintf(`{
		"query": "{ team(id:\"%s\") { issues(filter:{state:{type:{in:[\"unstarted\",\"backlog\"]}}}, first:50) { nodes { identifier title description priority state { name } labels { nodes { name } } } } } }"
	}`, teamID)

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBufferString(query))
	if err != nil {
		return nil, fmt.Errorf("creating Linear request: %w", err)
	}

	req.Header.Set("Authorization", apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("querying Linear: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading Linear response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Linear returned %d: %s", resp.StatusCode, string(body))
	}

	return parseLinearResponse(body)
}

// ParseLinearResponseForTest exports parseLinearResponse for testing.
func ParseLinearResponseForTest(body []byte) ([]LinearTicket, error) {
	return parseLinearResponse(body)
}

func parseLinearResponse(body []byte) ([]LinearTicket, error) {
	var result struct {
		Data struct {
			Team struct {
				Issues struct {
					Nodes []struct {
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
					} `json:"nodes"`
				} `json:"issues"`
			} `json:"team"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing Linear JSON: %w", err)
	}

	tickets := make([]LinearTicket, 0, len(result.Data.Team.Issues.Nodes))
	for _, node := range result.Data.Team.Issues.Nodes {
		labels := make([]string, 0, len(node.Labels.Nodes))
		for _, label := range node.Labels.Nodes {
			labels = append(labels, label.Name)
		}

		ticket := LinearTicket{
			ID:          node.Identifier,
			Title:       node.Title,
			Description: node.Description,
			Priority:    node.Priority,
			Labels:      labels,
			State:       node.State.Name,
			DependsOn:   parseDependencies(node.Description),
		}
		tickets = append(tickets, ticket)
	}

	log.Printf("fetched %d tickets from Linear", len(tickets))
	return tickets, nil
}

var (
	dependsOnPattern = regexp.MustCompile(`(?i)depends?\s+on[:\s]*\*?\*?\s*(JAM-\d+(?:\s*[,/&]\s*JAM-\d+)*)`)
	ticketIDPattern  = regexp.MustCompile(`JAM-\d+`)
)

// ParseDependenciesForTest exports parseDependencies for testing.
func ParseDependenciesForTest(description string) []string {
	return parseDependencies(description)
}

// parseDependencies extracts ticket IDs from "Depends on: JAM-X, JAM-Y".
func parseDependencies(description string) []string {
	matches := dependsOnPattern.FindAllStringSubmatch(description, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	deps := make([]string, 0, 4)

	for _, match := range matches {
		ids := ticketIDPattern.FindAllString(match[0], -1)
		for _, id := range ids {
			upper := strings.ToUpper(id)
			if seen[upper] {
				continue
			}
			seen[upper] = true
			deps = append(deps, upper)
		}
	}

	return deps
}
