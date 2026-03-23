package orchestrator

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/memory"
)

const extractionPromptTemplate = `Extract learnings from this agent's work session transcript.

## Ticket: %s

## Transcript (truncated):
%s

## Instructions
Analyse the transcript and extract:
- **facts**: specific things learned about code, modules, or processes (entity_name, entity_type, content, fact_type)
- **beliefs**: general opinions formed from this experience (content)
- **entities**: modules, files, or concepts encountered (name, type)

entity_type must be one of: module, file, pattern, tool, concept
fact_type must be one of: observation, preference, warning, technique

Respond with ONLY valid JSON — no markdown, no code fences, no explanation:
{"facts": [...], "beliefs": [...], "entities": [...]}

If nothing meaningful was learned, respond with:
{"facts": [], "beliefs": [], "entities": []}
`

// FlushSessionMemory extracts learnings from a completed session and
// stores them in the agent's knowledge graph. This is the orchestrator-
// driven fallback — agents also store memories directly via MCP tools
// during their session.
func FlushSessionMemory(
	ctx context.Context,
	extractor *agent.Agent,
	agentInstance *agent.Agent,
	ticket string,
	transcript string,
) {
	role := agentInstance.Role()

	graphStore := agentInstance.GraphStore()
	factStore := agentInstance.FactStore()
	episodeStore := agentInstance.EpisodeStore()

	if graphStore == nil || factStore == nil || episodeStore == nil {
		log.Printf("memory flush skipped for %s: missing memory stores", role)
		return
	}

	learnings := extractLearnings(ctx, extractor, ticket, transcript)
	if learnings == nil {
		return
	}

	err := memory.FlushLearnings(
		ctx, graphStore, factStore, episodeStore,
		*learnings, string(role), ticket, agentInstance.Embedder(),
	)
	if err != nil {
		log.Printf("memory flush failed for %s: %v", role, err)
	}
}

func extractLearnings(ctx context.Context, extractor *agent.Agent, ticket, transcript string) *memory.SessionLearnings {
	truncated := truncateTranscript(transcript, 2000)
	prompt := fmt.Sprintf(extractionPromptTemplate, ticket, truncated)

	result, err := extractor.DirectSession(ctx, prompt)
	if err != nil {
		log.Printf("learning extraction failed: %v", err)
		return nil
	}

	jsonStr := extractJSONObject(result.Transcript)
	if jsonStr == "" {
		return nil
	}

	learnings, err := memory.ParseLearningsJSON(jsonStr)
	if err != nil {
		log.Printf("failed to parse extracted learnings: %v", err)
		return nil
	}

	return &learnings
}

// TruncateTranscriptForTest exports truncateTranscript for testing.
func TruncateTranscriptForTest(text string, maxLen int) string {
	return truncateTranscript(text, maxLen)
}

// ExtractJSONObjectForTest exports extractJSONObject for testing.
func ExtractJSONObjectForTest(text string) string {
	return extractJSONObject(text)
}

func truncateTranscript(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen]
}

func extractJSONObject(text string) string {
	start := strings.Index(text, "{")
	if start == -1 {
		return ""
	}

	end := strings.LastIndex(text, "}")
	if end == -1 {
		return ""
	}

	return text[start : end+1]
}
