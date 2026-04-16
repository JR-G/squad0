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

// ChatBridge is an optional bridge for routing QuickChat through an
// alternative runtime with fallback support. When set, QuickChat
// delegates to the bridge for execution. The bridge receives the
// personality working directory *and* the persona system-prompt
// so it can run Claude with both the CLAUDE.md context and the
// --append-system-prompt persona anchor. Implemented by
// runtime.SessionBridge.
type ChatBridge interface {
	Chat(ctx context.Context, prompt, workDir, systemPrompt string) (string, error)
}

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
	chatBridge     ChatBridge
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

// HasMemoryStores reports whether all stores required by the post-
// session memory flush are wired. Used by startup to fail fast on a
// configuration mistake instead of silently dropping learnings every
// session.
func (agent *Agent) HasMemoryStores() bool {
	return agent.graphStore != nil && agent.factStore != nil && agent.episodeStore != nil
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
		Role:       agent.role,
		Model:      agent.model,
		Prompt:     prompt,
		WorkingDir: workingDir,
		Env:        agent.sessionEnv(),
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

// QuickChat runs a lightweight conversation session. Creates a temp
// directory with a CLAUDE.md containing the agent's personality.
// When a bridge is set, uses it for runtime selection and fallback.
// The personality CLAUDE.md is always created regardless of bridge.
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

	// Bridge path — personality dir + system prompt passed so the
	// bridge runs Claude with the CLAUDE.md, persona anchor, correct
	// model, and fallback support.
	if agent.chatBridge != nil {
		return agent.chatBridge.Chat(ctx, prompt, chatCtx.Dir(), chatCtx.SystemPrompt())
	}

	// Direct path — no bridge, use session.Run.
	cfg := SessionConfig{
		Role:         agent.role,
		Model:        chatModel,
		Prompt:       prompt,
		SystemPrompt: chatCtx.SystemPrompt(),
		WorkingDir:   chatCtx.Dir(),
		Env:          agent.sessionEnv(),
	}

	result, runErr := agent.session.Run(ctx, cfg)
	if runErr != nil {
		log.Printf("quick chat failed for %s: %v", agent.role, runErr)
		return "", runErr
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

// Name returns the agent's chosen display name from the chat
// roster, falling back to the role slug if the roster hasn't been
// set yet. Used everywhere history lines or identity references are
// rendered for other agents to read — never expose the role slug to
// the model if a real name exists.
func (agent *Agent) Name() string {
	agent.chatMu.Lock()
	defer agent.chatMu.Unlock()
	if name, ok := agent.chatRoster[agent.role]; ok && name != "" {
		return name
	}
	return string(agent.role)
}

// SetBridge connects a ChatBridge for routing QuickChat through a
// persistent session or alternative runtime. Optional — when not set,
// QuickChat spawns fresh processes as before.
func (agent *Agent) SetBridge(bridge ChatBridge) {
	agent.chatBridge = bridge
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
		Env:        agent.sessionEnv(),
	}

	return agent.session.Run(ctx, cfg)
}

// sessionEnv returns the env vars every spawned claude subprocess
// needs: GH_TOKEN if set, and SQUAD0_MEMORY_DB pointing at this
// agent's SQLite file. The memory env var is the bridge between the
// single user-scope MCP registration and per-agent DBs — without it
// the squad0-memory binary refuses to start.
func (agent *Agent) sessionEnv() map[string]string {
	env := map[string]string{}
	if agent.ghToken != "" {
		env["GH_TOKEN"] = agent.ghToken
	}
	if agent.dbPath != "" {
		env["SQUAD0_MEMORY_DB"] = agent.dbPath
	}
	if len(env) == 0 {
		return nil
	}
	return env
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
