package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/memory"
)

// extractionMaxAttempts is the cap on retries for the Claude-driven
// learning extraction. Most failures are transient (rate limit,
// network blip) and recover within seconds; a hard cap keeps a
// totally-down model from blocking the post-session goroutine.
const extractionMaxAttempts = 3

// extractionRetryDelay is the base backoff between extraction
// attempts. Doubles each retry. Declared as var so tests can shrink
// it without spending real wall-clock time on retries.
var extractionRetryDelay = 2 * time.Second

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
		// Loud — startup assertion should have caught this; if it
		// didn't, every session for this agent is silently leaking
		// learnings. Use ERROR so the alerter can pick it up.
		log.Printf("ERROR: memory flush skipped for %s — memory stores not wired (learnings lost for ticket %s)", role, ticket)
		return
	}

	learnings := extractLearnings(ctx, extractor, ticket, transcript)
	if learnings == nil {
		log.Printf("WARN: memory flush for %s on %s produced no learnings", role, ticket)
		return
	}

	err := memory.FlushLearnings(
		ctx, graphStore, factStore, episodeStore,
		*learnings, string(role), ticket, agentInstance.Embedder(),
	)
	if err != nil {
		log.Printf("ERROR: memory flush failed for %s on %s: %v", role, ticket, err)
	}
}

func extractLearnings(ctx context.Context, extractor *agent.Agent, ticket, transcript string) *memory.SessionLearnings {
	truncated := truncateTranscript(transcript, 2000)
	prompt := fmt.Sprintf(extractionPromptTemplate, ticket, truncated)

	var lastErr error
	for attempt := 1; attempt <= extractionMaxAttempts; attempt++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			log.Printf("learning extraction abandoned: %v", ctxErr)
			return nil
		}

		result, err := extractor.DirectSession(ctx, prompt)
		if err == nil {
			return parseExtractedLearnings(result.Transcript)
		}

		lastErr = err
		log.Printf("learning extraction attempt %d/%d failed: %v", attempt, extractionMaxAttempts, err)

		if !isRetryableExtractionError(err) || attempt == extractionMaxAttempts {
			break
		}

		backoff := extractionRetryDelay << (attempt - 1)
		select {
		case <-ctx.Done():
			log.Printf("learning extraction abandoned: %v", ctx.Err())
			return nil
		case <-time.After(backoff):
		}
	}

	log.Printf("ERROR: learning extraction gave up after %d attempts: %v", extractionMaxAttempts, lastErr)
	return nil
}

func parseExtractedLearnings(transcript string) *memory.SessionLearnings {
	jsonStr := extractJSONObject(transcript)
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

// isRetryableExtractionError reports whether err looks transient
// enough to be worth retrying. A cancelled context is never retryable;
// most other Claude/Ollama failures (rate limit, 5xx, dropped
// connection) are. The check is conservative: if in doubt, retry,
// because a missed flush silently degrades the agent's knowledge.
func isRetryableExtractionError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return true
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
