package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// FlushConfig holds configuration for post-session memory extraction.
type FlushConfig struct {
	MaxFactsPerSession   int
	MaxBeliefsPerSession int
}

// DefaultFlushConfig returns sensible defaults for memory flushing.
func DefaultFlushConfig() FlushConfig {
	return FlushConfig{
		MaxFactsPerSession:   10,
		MaxBeliefsPerSession: 5,
	}
}

// SessionLearnings holds the extracted learnings from a session transcript.
type SessionLearnings struct {
	Facts    []ExtractedFact
	Beliefs  []ExtractedBelief
	Entities []ExtractedEntity
}

// ExtractedFact is a fact parsed from the session transcript.
type ExtractedFact struct {
	EntityName string `json:"entity_name"`
	EntityType string `json:"entity_type"`
	Content    string `json:"content"`
	FactType   string `json:"fact_type"`
}

// ExtractedBelief is a belief parsed from the session transcript.
type ExtractedBelief struct {
	Content string `json:"content"`
}

// ExtractedEntity is an entity mentioned in the session.
type ExtractedEntity struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// FlushLearnings stores the extracted learnings from a session transcript
// into the agent's knowledge graph.
func FlushLearnings(
	ctx context.Context,
	graphStore *GraphStore,
	factStore *FactStore,
	episodeStore *EpisodeStore,
	learnings SessionLearnings,
	agentName string,
	ticket string,
	embedder *Embedder,
) error {
	for _, extracted := range learnings.Entities {
		entityType := EntityType(extracted.Type)
		_, _, err := graphStore.FindOrCreateEntity(ctx, entityType, extracted.Name, "")
		if err != nil {
			return fmt.Errorf("creating entity %s: %w", extracted.Name, err)
		}
	}

	for _, extracted := range learnings.Facts {
		entityType := EntityType(extracted.EntityType)
		entity, _, err := graphStore.FindOrCreateEntity(ctx, entityType, extracted.EntityName, "")
		if err != nil {
			return fmt.Errorf("creating entity for fact: %w", err)
		}

		_, err = factStore.CreateFact(ctx, Fact{
			EntityID:   entity.ID,
			Content:    extracted.Content,
			Type:       FactType(extracted.FactType),
			Confidence: 0.5,
		})
		if err != nil {
			return fmt.Errorf("storing fact: %w", err)
		}
	}

	for _, extracted := range learnings.Beliefs {
		_, err := factStore.CreateBelief(ctx, Belief{
			Content:    extracted.Content,
			Confidence: 0.5,
		})
		if err != nil {
			return fmt.Errorf("storing belief: %w", err)
		}
	}

	summary := buildEpisodeSummary(learnings)
	episode := Episode{
		Agent:   agentName,
		Ticket:  ticket,
		Summary: summary,
		Outcome: OutcomeSuccess,
	}

	episode.Embedding = tryEmbed(ctx, embedder, summary)

	_, err := episodeStore.CreateEpisode(ctx, episode)
	if err != nil {
		return fmt.Errorf("storing episode: %w", err)
	}

	return nil
}

// ParseLearningsJSON parses a JSON string of learnings extracted by the
// orchestrator from a session transcript.
func ParseLearningsJSON(jsonStr string) (SessionLearnings, error) {
	var learnings SessionLearnings

	if err := json.Unmarshal([]byte(jsonStr), &learnings); err != nil {
		return SessionLearnings{}, fmt.Errorf("parsing learnings JSON: %w", err)
	}

	return learnings, nil
}

func appendSummaryPart(builder *strings.Builder, count int, format string) {
	if count == 0 {
		return
	}

	if builder.Len() > 0 {
		builder.WriteString(", ")
	}

	fmt.Fprintf(builder, format, count)
}

func tryEmbed(ctx context.Context, embedder *Embedder, text string) []float32 {
	if embedder == nil {
		return nil
	}

	embedding, err := embedder.Embed(ctx, text)
	if err != nil {
		return nil
	}

	return embedding
}

func buildEpisodeSummary(learnings SessionLearnings) string {
	var builder strings.Builder

	if len(learnings.Facts) > 0 {
		fmt.Fprintf(&builder, "Learned %d facts", len(learnings.Facts))
	}

	appendSummaryPart(&builder, len(learnings.Beliefs), "formed %d beliefs")
	appendSummaryPart(&builder, len(learnings.Entities), "encountered %d entities")

	if builder.Len() == 0 {
		return "Session completed with no extracted learnings"
	}

	return builder.String()
}
