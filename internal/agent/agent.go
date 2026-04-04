package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/JR-G/squad0/internal/memory"
)

const chatModel = "claude-haiku-4-5-20251001"

// Agent represents a squad0 team member with a persistent identity.
type Agent struct {
	role           Role
	model          string
	session        *Session
	loader         *PersonalityLoader
	retriever      *memory.Retriever
	agentDB        *memory.DB
	graphStore     *memory.GraphStore
	factStore      *memory.FactStore
	episodeStore   *memory.EpisodeStore
	embedder       *memory.Embedder
	dbPath         string
	MCPConfigPath  string
	ghToken        string // If set, passed as GH_TOKEN env var only — never on disk.
	defaultWorkDir string // CWD for DirectSession — target repo access.
	chatMu         sync.Mutex
	chatRoster     map[Role]string
	chatBeliefs    []string
	chatVoice      string
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

// SetMemoryStores sets the graph and fact stores for post-session
// memory flush. Called after construction because these stores are
// created externally.
func (agent *Agent) SetMemoryStores(graphStore *memory.GraphStore, factStore *memory.FactStore) {
	agent.graphStore = graphStore
	agent.factStore = factStore
}

// SetDBPath stores the filesystem path to the agent's SQLite database
// so the orchestrator can configure MCP servers per session.
func (agent *Agent) SetDBPath(path string) {
	agent.dbPath = path
}

// Role returns the agent's role.
func (agent *Agent) Role() Role {
	return agent.role
}

// DBPath returns the filesystem path to the agent's database.
func (agent *Agent) DBPath() string {
	return agent.dbPath
}

// GraphStore returns the agent's graph store for memory operations.
func (agent *Agent) GraphStore() *memory.GraphStore {
	return agent.graphStore
}

// FactStore returns the agent's fact store for memory operations.
func (agent *Agent) FactStore() *memory.FactStore {
	return agent.factStore
}

// EpisodeStore returns the agent's episode store.
func (agent *Agent) EpisodeStore() *memory.EpisodeStore {
	return agent.episodeStore
}

// Embedder returns the agent's text embedder.
func (agent *Agent) Embedder() *memory.Embedder {
	return agent.embedder
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
		Role:          agent.role,
		Model:         agent.model,
		Prompt:        prompt,
		WorkingDir:    workingDir,
		MCPConfigPath: agent.MCPConfigPath,
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

// QuickChat runs a lightweight Claude Code session for conversation.
// Creates a temp directory with a CLAUDE.md containing the agent's
// personality — Claude Code reads this as project context, making
// the identity part of its operating instructions rather than a
// user prompt it can ignore.
func (agent *Agent) QuickChat(ctx context.Context, prompt string) (string, error) {
	agent.chatMu.Lock()
	roster := agent.chatRoster
	beliefs := agent.chatBeliefs
	voice := agent.chatVoice
	agent.chatMu.Unlock()

	chatCtx, err := NewChatContext(agent.role, roster, beliefs, voice)
	if err != nil {
		log.Printf("quick chat context failed for %s: %v", agent.role, err)
		return "", err
	}
	defer chatCtx.Cleanup()

	cfg := SessionConfig{
		Role:       agent.role,
		Model:      chatModel,
		Prompt:     prompt,
		WorkingDir: chatCtx.Dir(),
	}

	result, err := agent.session.Run(ctx, cfg)
	if err != nil {
		log.Printf("quick chat failed for %s: %v", agent.role, err)
		return "", err
	}

	return result.Transcript, nil
}

// SetChatContext provides the roster, beliefs, and voice text for
// CLAUDE.md generation during QuickChat sessions. Thread-safe.
func (agent *Agent) SetChatContext(roster map[Role]string, beliefs []string, voiceText string) {
	agent.chatMu.Lock()
	defer agent.chatMu.Unlock()
	agent.chatRoster = roster
	agent.chatBeliefs = beliefs
	agent.chatVoice = voiceText
}

// SetGHToken sets a custom GitHub token for this agent's sessions.
// The token is passed as a process env var only — never written to disk.
func (agent *Agent) SetGHToken(token string) {
	agent.ghToken = token
}

// SetDefaultWorkDir sets the working directory for QuickChat and
// DirectSession. Without this, sessions inherit the squad0 process
// CWD and pick up squad0's own CLAUDE.md.
func (agent *Agent) SetDefaultWorkDir(dir string) {
	agent.defaultWorkDir = dir
}

// DirectSession runs a clean Claude Code session with the agent's own
// model. No personality wrapping, no memory retrieval. Used for
// structured tasks like querying Linear where the prompt should not
// be buried in other context.
func (agent *Agent) DirectSession(ctx context.Context, prompt string) (SessionResult, error) {
	cfg := SessionConfig{
		Role:       agent.role,
		Model:      agent.model,
		Prompt:     prompt,
		WorkingDir: agent.defaultWorkDir,
		Env:        agent.envWithGHToken(),
	}

	return agent.session.Run(ctx, cfg)
}

func (agent *Agent) envWithGHToken() map[string]string {
	if agent.ghToken == "" {
		return nil
	}
	return map[string]string{"GH_TOKEN": agent.ghToken}
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
	outcome := DetermineOutcome(result)

	episode := memory.Episode{
		Agent:   string(agent.role),
		Summary: TruncateSummary(result.Transcript, 500),
		Outcome: outcome,
	}

	embedding, err := agent.embedder.Embed(ctx, taskDescription+" "+episode.Summary)
	if err == nil {
		episode.Embedding = embedding
	}

	_, err = agent.episodeStore.CreateEpisode(ctx, episode)
	return err
}

// DetermineOutcome infers the session outcome from the exit code and
// any error messages in the stream output.
func DetermineOutcome(result SessionResult) memory.Outcome {
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

// TruncateSummary shortens text to the given maximum length.
func TruncateSummary(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen]
}
