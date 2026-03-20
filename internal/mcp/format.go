package mcp

import (
	"fmt"
	"strings"

	"github.com/JR-G/squad0/internal/memory"
)

func formatRetrievalContext(memCtx memory.RetrievalContext) string {
	var builder strings.Builder

	if len(memCtx.Facts) == 0 && len(memCtx.Beliefs) == 0 && len(memCtx.Episodes) == 0 {
		return "No relevant memories found."
	}

	formatRecalledFacts(&builder, memCtx.Facts)
	formatRecalledBeliefs(&builder, memCtx.Beliefs)
	formatRecalledEpisodes(&builder, memCtx.Episodes)
	formatRecalledEntities(&builder, memCtx.Entities)

	return builder.String()
}

func formatRecalledFacts(builder *strings.Builder, facts []memory.Fact) {
	if len(facts) == 0 {
		return
	}

	builder.WriteString("Facts:\n")
	for _, fact := range facts {
		fmt.Fprintf(builder, "  [%s, confidence: %.1f] %s\n", fact.Type, fact.Confidence, fact.Content)
	}
	builder.WriteString("\n")
}

func formatRecalledBeliefs(builder *strings.Builder, beliefs []memory.Belief) {
	if len(beliefs) == 0 {
		return
	}

	builder.WriteString("Beliefs:\n")
	for _, belief := range beliefs {
		fmt.Fprintf(builder, "  [confidence: %.1f] %s\n", belief.Confidence, belief.Content)
	}
	builder.WriteString("\n")
}

func formatRecalledEpisodes(builder *strings.Builder, episodes []memory.Episode) {
	if len(episodes) == 0 {
		return
	}

	builder.WriteString("Past sessions:\n")
	for _, episode := range episodes {
		fmt.Fprintf(builder, "  [%s] %s — %s\n", episode.Outcome, episode.Ticket, episode.Summary)
	}
	builder.WriteString("\n")
}

func formatRecalledEntities(builder *strings.Builder, entities []memory.Entity) {
	if len(entities) == 0 {
		return
	}

	builder.WriteString("Related entities:\n")
	for _, entity := range entities {
		fmt.Fprintf(builder, "  %s (%s)", entity.Name, entity.Type)
		if entity.Summary != "" {
			fmt.Fprintf(builder, " — %s", entity.Summary)
		}
		builder.WriteString("\n")
	}
}

func formatEntityKnowledge(entity memory.Entity, facts []memory.Fact, related []memory.Entity) string {
	var builder strings.Builder

	fmt.Fprintf(&builder, "%s (%s)\n", entity.Name, entity.Type)
	if entity.Summary != "" {
		fmt.Fprintf(&builder, "%s\n", entity.Summary)
	}
	builder.WriteString("\n")

	if len(facts) > 0 {
		builder.WriteString("Known facts:\n")
		for _, fact := range facts {
			fmt.Fprintf(&builder, "  [%s, confidence: %.1f] %s\n", fact.Type, fact.Confidence, fact.Content)
		}
		builder.WriteString("\n")
	}

	if len(related) > 0 {
		builder.WriteString("Connected to:\n")
		for _, rel := range related {
			fmt.Fprintf(&builder, "  %s (%s)\n", rel.Name, rel.Type)
		}
	}

	return builder.String()
}
