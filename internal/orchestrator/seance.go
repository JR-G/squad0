package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/pipeline"
)

// BuildSeanceContext queries the project episode store for prior work
// on the given ticket by other agents. Also pulls beliefs from all
// agents' fact stores and handoff summaries. Returns a prompt section
// describing what previous agents found, or empty string if no prior
// work exists.
func BuildSeanceContext(
	ctx context.Context,
	episodeStore *memory.EpisodeStore,
	ticket string,
	currentAgent agent.Role,
) string {
	return buildSeanceContextFull(ctx, episodeStore, nil, nil, ticket, currentAgent)
}

// BuildSeanceContextFull queries episodes, agent fact stores, and
// handoff history to build a rich context section for reassigned
// tickets. This is the full seance — pulling knowledge from all
// available sources.
func BuildSeanceContextFull(
	ctx context.Context,
	episodeStore *memory.EpisodeStore,
	agentFactStores map[agent.Role]*memory.FactStore,
	handoffStore *pipeline.HandoffStore,
	ticket string,
	currentAgent agent.Role,
) string {
	return buildSeanceContextFull(ctx, episodeStore, agentFactStores, handoffStore, ticket, currentAgent)
}

func buildSeanceContextFull(
	ctx context.Context,
	episodeStore *memory.EpisodeStore,
	agentFactStores map[agent.Role]*memory.FactStore,
	handoffStore *pipeline.HandoffStore,
	ticket string,
	currentAgent agent.Role,
) string {
	var builder strings.Builder

	appendEpisodes(ctx, &builder, episodeStore, ticket, currentAgent)
	appendHandoffs(ctx, &builder, handoffStore, ticket)
	appendCrossAgentBeliefs(ctx, &builder, agentFactStores, ticket, currentAgent)

	if builder.Len() == 0 {
		return ""
	}

	header := "## Previous Work on This Ticket\n\n"
	footer := "Use this context to avoid repeating their mistakes and build on what they learned.\n\n"

	return header + builder.String() + footer
}

func appendEpisodes(
	ctx context.Context,
	builder *strings.Builder,
	store *memory.EpisodeStore,
	ticket string,
	currentAgent agent.Role,
) {
	if store == nil {
		return
	}

	episodes, err := store.EpisodesByTicket(ctx, ticket)
	if err != nil {
		return
	}

	var relevant []memory.Episode
	for _, episode := range episodes {
		if agent.Role(episode.Agent) != currentAgent {
			relevant = append(relevant, episode)
		}
	}

	if len(relevant) == 0 {
		return
	}

	builder.WriteString("Other agents have worked on this ticket before. Here is what they found:\n\n")
	for _, episode := range relevant {
		fmt.Fprintf(builder, "**%s** (%s): %s\n\n", episode.Agent, episode.Outcome, episode.Summary)
	}
}

func appendHandoffs(
	ctx context.Context,
	builder *strings.Builder,
	store *pipeline.HandoffStore,
	ticket string,
) {
	if store == nil {
		return
	}

	handoffs, err := store.AllForTicket(ctx, ticket)
	if err != nil || len(handoffs) == 0 {
		return
	}

	builder.WriteString("### Session Handoffs\n\n")
	for _, handoff := range handoffs {
		fmt.Fprintf(builder, "**%s** (%s): %s\n", handoff.Agent, handoff.Status, handoff.Summary)
		if handoff.Blockers != "" {
			fmt.Fprintf(builder, "  Blockers: %s\n", handoff.Blockers)
		}
		builder.WriteString("\n")
	}
}

func appendCrossAgentBeliefs(
	ctx context.Context,
	builder *strings.Builder,
	agentFactStores map[agent.Role]*memory.FactStore,
	ticket string,
	currentAgent agent.Role,
) {
	if len(agentFactStores) == 0 {
		return
	}

	var beliefs []string

	for role, store := range agentFactStores {
		if role == currentAgent {
			continue
		}
		roleBeliefs := searchBeliefsForTicket(ctx, store, ticket)
		for _, belief := range roleBeliefs {
			beliefs = append(beliefs, fmt.Sprintf("**%s**: %s", role, belief))
		}
	}

	if len(beliefs) == 0 {
		return
	}

	builder.WriteString("### Beliefs from Other Engineers\n\n")
	for _, belief := range beliefs {
		fmt.Fprintf(builder, "%s\n\n", belief)
	}
}

// searchBeliefsForTicket uses FTS5 to find beliefs mentioning the
// ticket ID. Returns the content of matching beliefs.
func searchBeliefsForTicket(ctx context.Context, store *memory.FactStore, ticket string) []string {
	beliefs, err := store.SearchBeliefsByKeyword(ctx, ticket, 3)
	if err != nil {
		return nil
	}
	return beliefs
}
