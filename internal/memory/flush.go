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

const jsonNull = "null"

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

// UnmarshalJSON tolerantly decodes the learnings JSON the extraction
// model produces. The model is asked for
//
//	{"facts": [...], "beliefs": [...], "entities": [...]}
//
// but in practice it sometimes returns a plain string, an array of
// strings, or a mix of shapes. Rather than fail the whole flush on a
// schema mismatch — losing all the agent's learnings — each field is
// parsed flexibly here and normalised into the strict struct form.
func (learnings *SessionLearnings) UnmarshalJSON(data []byte) error {
	var raw struct {
		Facts    json.RawMessage `json:"facts"`
		Beliefs  json.RawMessage `json:"beliefs"`
		Entities json.RawMessage `json:"entities"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	facts, err := parseFlexibleFacts(raw.Facts)
	if err != nil {
		return fmt.Errorf("facts: %w", err)
	}
	beliefs, err := parseFlexibleBeliefs(raw.Beliefs)
	if err != nil {
		return fmt.Errorf("beliefs: %w", err)
	}
	entities, err := parseFlexibleEntities(raw.Entities)
	if err != nil {
		return fmt.Errorf("entities: %w", err)
	}

	learnings.Facts = facts
	learnings.Beliefs = beliefs
	learnings.Entities = entities
	return nil
}

func parseFlexibleFacts(data json.RawMessage) ([]ExtractedFact, error) {
	if len(data) == 0 || string(data) == jsonNull {
		return nil, nil
	}

	// Strict form: array of objects.
	var structured []ExtractedFact
	if err := json.Unmarshal(data, &structured); err == nil {
		return structured, nil
	}

	// Degenerate forms: string or array of strings. A bare fact
	// without entity context is low signal but better than dropping
	// the whole flush.
	contents, err := parseFlexibleStrings(data)
	if err != nil {
		return nil, err
	}
	result := make([]ExtractedFact, 0, len(contents))
	for _, content := range contents {
		result = append(result, ExtractedFact{
			EntityName: "session",
			EntityType: "concept",
			Content:    content,
			FactType:   "observation",
		})
	}
	return result, nil
}

func parseFlexibleBeliefs(data json.RawMessage) ([]ExtractedBelief, error) {
	if len(data) == 0 || string(data) == jsonNull {
		return nil, nil
	}

	var structured []ExtractedBelief
	if err := json.Unmarshal(data, &structured); err == nil {
		return structured, nil
	}

	contents, err := parseFlexibleStrings(data)
	if err != nil {
		return nil, err
	}
	result := make([]ExtractedBelief, 0, len(contents))
	for _, content := range contents {
		result = append(result, ExtractedBelief{Content: content})
	}
	return result, nil
}

func parseFlexibleEntities(data json.RawMessage) ([]ExtractedEntity, error) {
	if len(data) == 0 || string(data) == jsonNull {
		return nil, nil
	}

	var structured []ExtractedEntity
	if err := json.Unmarshal(data, &structured); err == nil {
		return structured, nil
	}

	names, err := parseFlexibleStrings(data)
	if err != nil {
		return nil, err
	}
	result := make([]ExtractedEntity, 0, len(names))
	for _, name := range names {
		result = append(result, ExtractedEntity{Name: name, Type: "concept"})
	}
	return result, nil
}

// parseFlexibleStrings decodes data as either a single string or an
// array of strings. Returns an error only when the shape is neither.
func parseFlexibleStrings(data json.RawMessage) ([]string, error) {
	single, ok := decodeSingleString(data)
	if ok && single == "" {
		return nil, nil
	}
	if ok {
		return []string{single}, nil
	}

	var list []string
	if err := json.Unmarshal(data, &list); err == nil {
		return list, nil
	}

	return nil, fmt.Errorf("unrecognised shape: %s", string(data))
}

func decodeSingleString(data json.RawMessage) (string, bool) {
	var single string
	if err := json.Unmarshal(data, &single); err != nil {
		return "", false
	}
	return strings.TrimSpace(single), true
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
