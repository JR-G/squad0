package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/JR-G/squad0/internal/memory"
)

// Agent represents a squad0 team member with a persistent identity.
type Agent struct {
	role         Role
	model        string
	session      *Session
	loader       *PersonalityLoader
	retriever    *memory.Retriever
	agentDB      *memory.DB
	episodeStore *memory.EpisodeStore
	embedder     *memory.Embedder
}

// NewAgent creates an Agent with all dependencies injected.
func NewAgent(
	role Role,
	model string,
	session *Session,
	loader *PersonalityLoader,
	retriever *memory.Retriever,
	agentDB *memory.DB,
	episodeStore *memory.EpisodeStore,
	embedder *memory.Embedder,
) *Agent {
	return &Agent{
		role:         role,
		model:        model,
		session:      session,
		loader:       loader,
		retriever:    retriever,
		agentDB:      agentDB,
		episodeStore: episodeStore,
		embedder:     embedder,
	}
}

// Role returns the agent's role.
func (agent *Agent) Role() Role {
	return agent.role
}

// ExecuteTask runs a complete agent session for the given task: assembles
// the prompt with personality and memories, runs the Claude Code session,
// and stores the episode in the knowledge graph.
func (agent *Agent) ExecuteTask(ctx context.Context, taskDescription string, filePaths []string, workingDir string) (SessionResult, error) {
	prompt, err := agent.assemblePrompt(ctx, taskDescription, filePaths)
	if err != nil {
		return SessionResult{}, fmt.Errorf("assembling prompt for %s: %w", agent.role, err)
	}

	cfg := SessionConfig{
		Role:       agent.role,
		Model:      agent.model,
		Prompt:     prompt,
		WorkingDir: workingDir,
	}

	result, sessionErr := agent.session.Run(ctx, cfg)

	storeErr := agent.storeEpisode(ctx, taskDescription, result)
	if storeErr != nil && sessionErr == nil {
		return result, fmt.Errorf("storing episode for %s: %w", agent.role, storeErr)
	}

	if sessionErr != nil {
		return result, sessionErr
	}

	return result, nil
}

func (agent *Agent) assemblePrompt(ctx context.Context, taskDescription string, filePaths []string) (string, error) {
	personality, err := agent.loader.LoadBase(agent.role)
	if err != nil {
		return "", fmt.Errorf("loading personality: %w", err)
	}

	memCtx, err := RetrieveMemoryContext(ctx, agent.retriever, taskDescription, filePaths)
	if err != nil {
		return "", fmt.Errorf("retrieving memory: %w", err)
	}

	return AssemblePrompt(personality, memCtx, taskDescription), nil
}

func (agent *Agent) storeEpisode(ctx context.Context, taskDescription string, result SessionResult) error {
	outcome := determineOutcome(result)

	episode := memory.Episode{
		Agent:   string(agent.role),
		Summary: truncateSummary(result.Transcript, 500),
		Outcome: outcome,
	}

	embedding, err := agent.embedder.Embed(ctx, taskDescription+" "+episode.Summary)
	if err == nil {
		episode.Embedding = embedding
	}

	_, err = agent.episodeStore.CreateEpisode(ctx, episode)
	return err
}

func determineOutcome(result SessionResult) memory.Outcome {
	if result.ExitCode != 0 {
		return memory.OutcomeFailure
	}

	for _, msg := range result.Messages {
		if msg.Type != "error" {
			continue
		}

		var errorContent string
		if err := json.Unmarshal(msg.Content, &errorContent); err != nil {
			continue
		}

		if errorContent != "" {
			return memory.OutcomePartial
		}
	}

	return memory.OutcomeSuccess
}

func truncateSummary(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen]
}
