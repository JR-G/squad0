package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/JR-G/squad0/internal/memory"
)

// PersonalityLoader reads base personality files and enriches them with
// retrieved memory context.
type PersonalityLoader struct {
	personalityDir string
}

// NewPersonalityLoader creates a PersonalityLoader that reads personality
// files from the given directory.
func NewPersonalityLoader(personalityDir string) *PersonalityLoader {
	return &PersonalityLoader{personalityDir: personalityDir}
}

// LoadBase reads the base personality markdown file for the given role.
func (loader *PersonalityLoader) LoadBase(role Role) (string, error) {
	path := filepath.Join(loader.personalityDir, role.PersonalityFile())

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading personality file for %s: %w", role, err)
	}

	return string(data), nil
}

// AssemblePrompt builds the full agent prompt by combining the base
// personality, retrieved memory context, and the current task description.
func AssemblePrompt(personality string, memCtx memory.RetrievalContext, taskDescription string) string {
	var builder strings.Builder

	builder.WriteString("# Personality\n\n")
	builder.WriteString(personality)
	builder.WriteString("\n\n")

	writeMemorySection(&builder, memCtx)

	builder.WriteString("# Current Task\n\n")
	builder.WriteString(taskDescription)
	builder.WriteString("\n")

	return builder.String()
}

func writeMemorySection(builder *strings.Builder, memCtx memory.RetrievalContext) {
	if len(memCtx.Facts) == 0 && len(memCtx.Beliefs) == 0 && len(memCtx.Episodes) == 0 {
		return
	}

	builder.WriteString("# Relevant Memory\n\n")

	writeFactsSection(builder, memCtx.Facts)
	writeBeliefsSection(builder, memCtx.Beliefs)
	writeEpisodesSection(builder, memCtx.Episodes)
}

func writeFactsSection(builder *strings.Builder, facts []memory.Fact) {
	if len(facts) == 0 {
		return
	}

	builder.WriteString("## Known Facts\n\n")
	for _, fact := range facts {
		fmt.Fprintf(builder, "- [%s] (confidence: %.1f) %s\n", fact.Type, fact.Confidence, fact.Content)
	}
	builder.WriteString("\n")
}

func writeBeliefsSection(builder *strings.Builder, beliefs []memory.Belief) {
	if len(beliefs) == 0 {
		return
	}

	builder.WriteString("## Beliefs\n\n")
	for _, belief := range beliefs {
		fmt.Fprintf(builder, "- (confidence: %.1f, confirmed: %d, contradicted: %d) %s\n",
			belief.Confidence, belief.Confirmations, belief.Contradictions, belief.Content)
	}
	builder.WriteString("\n")
}

func writeEpisodesSection(builder *strings.Builder, episodes []memory.Episode) {
	if len(episodes) == 0 {
		return
	}

	builder.WriteString("## Recent Sessions\n\n")
	for _, episode := range episodes {
		fmt.Fprintf(builder, "- [%s] %s — %s\n", episode.Outcome, episode.Ticket, episode.Summary)
	}
	builder.WriteString("\n")
}

// RetrieveMemoryContext loads relevant memories for a session using the
// retrieval pipeline.
func RetrieveMemoryContext(ctx context.Context, retriever *memory.Retriever, taskDescription string, filePaths []string) (memory.RetrievalContext, error) {
	memCtx, err := retriever.Retrieve(ctx, taskDescription, filePaths)
	if err != nil {
		return memory.RetrievalContext{}, fmt.Errorf("retrieving memory context: %w", err)
	}

	return memCtx, nil
}
