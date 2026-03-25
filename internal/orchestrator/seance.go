package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/memory"
)

// BuildSeanceContext queries the project episode store for prior work
// on the given ticket by other agents. Returns a prompt section
// describing what previous agents found, or empty string if no prior
// work exists.
func BuildSeanceContext(ctx context.Context, episodeStore *memory.EpisodeStore, ticket string, currentAgent agent.Role) string {
	if episodeStore == nil {
		return ""
	}

	episodes, err := episodeStore.EpisodesByTicket(ctx, ticket)
	if err != nil {
		return ""
	}

	var relevant []memory.Episode
	for _, episode := range episodes {
		if agent.Role(episode.Agent) != currentAgent {
			relevant = append(relevant, episode)
		}
	}

	if len(relevant) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("## Previous Work on This Ticket\n\n")
	builder.WriteString("Other agents have worked on this ticket before. Here is what they found:\n\n")

	for _, episode := range relevant {
		fmt.Fprintf(&builder, "**%s** (%s): %s\n\n", episode.Agent, episode.Outcome, episode.Summary)
	}

	builder.WriteString("Use this context to avoid repeating their mistakes and build on what they learned.\n\n")

	return builder.String()
}
