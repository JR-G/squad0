package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JR-G/squad0/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_ValidFile_ReturnsConfig(t *testing.T) {
	t.Parallel()

	path := writeTestTOML(t, validTOML)

	cfg, err := config.Load(path)

	require.NoError(t, err)
	assert.Equal(t, "squad0", cfg.Project.Name)
	assert.Equal(t, "github.com/JR-G/squad0", cfg.Project.Repo)
	assert.Equal(t, 3, cfg.Agents.MaxParallel)
	assert.Equal(t, "claude-opus-4-6", cfg.Agents.Models.TechLead)
	assert.Equal(t, "claude-sonnet-4-6", cfg.Agents.Models.Engineer)
	assert.Equal(t, 80, cfg.Quality.CoverageThreshold)
	assert.Equal(t, "ollama", cfg.Embeddings.Provider)
	assert.Contains(t, cfg.Slack.Channels, "commands")
}

func TestLoad_MissingFile_ReturnsError(t *testing.T) {
	t.Parallel()

	_, err := config.Load("/nonexistent/path/squad0.toml")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading config file")
}

func TestLoad_InvalidTOML_ReturnsError(t *testing.T) {
	t.Parallel()

	path := writeTestTOML(t, "this is not [valid toml ===")

	_, err := config.Load(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing config file")
}

func TestLoad_PartialFile_UsesDefaults(t *testing.T) {
	t.Parallel()

	partial := `
[project]
name = "custom-project"
`
	path := writeTestTOML(t, partial)

	cfg, err := config.Load(path)

	require.NoError(t, err)
	assert.Equal(t, "custom-project", cfg.Project.Name)
	assert.Equal(t, 3, cfg.Agents.MaxParallel)
	assert.Equal(t, "claude-opus-4-6", cfg.Agents.Models.TechLead)
	assert.Equal(t, 80, cfg.Quality.CoverageThreshold)
	assert.Equal(t, ".worktrees", cfg.Worktree.BaseDir)
	assert.Equal(t, "ollama", cfg.Embeddings.Provider)
	assert.Equal(t, "0 9 * * *", cfg.Rituals.StandupCron)
}

func TestLoad_EmptyFile_UsesAllDefaults(t *testing.T) {
	t.Parallel()

	path := writeTestTOML(t, "")

	cfg, err := config.Load(path)

	require.NoError(t, err)
	defaults := config.DefaultConfig()
	assert.Equal(t, defaults.Project.Name, cfg.Project.Name)
	assert.Equal(t, defaults.Agents.MaxParallel, cfg.Agents.MaxParallel)
	assert.Equal(t, defaults.Agents.Models.PM, cfg.Agents.Models.PM)
	assert.Equal(t, defaults.Quality.CoverageThreshold, cfg.Quality.CoverageThreshold)
}

func TestValidate_InvalidFields_ReturnsError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		modify  func(*config.Config)
		wantErr string
	}{
		{
			name:    "empty project name",
			modify:  func(cfg *config.Config) { cfg.Project.Name = "" },
			wantErr: "project name",
		},
		{
			name:    "max_parallel too low",
			modify:  func(cfg *config.Config) { cfg.Agents.MaxParallel = 0 },
			wantErr: "max_parallel",
		},
		{
			name:    "max_parallel too high",
			modify:  func(cfg *config.Config) { cfg.Agents.MaxParallel = 11 },
			wantErr: "max_parallel",
		},
		{
			name:    "negative cooldown",
			modify:  func(cfg *config.Config) { cfg.Agents.CooldownSeconds = -1 },
			wantErr: "cooldown_seconds",
		},
		{
			name:    "zero ticket_batch_size",
			modify:  func(cfg *config.Config) { cfg.Agents.TicketBatchSize = 0 },
			wantErr: "ticket_batch_size",
		},
		{
			name:    "zero personality_regen_every",
			modify:  func(cfg *config.Config) { cfg.Agents.PersonalityRegenEvery = 0 },
			wantErr: "personality_regen_every",
		},
		{
			name:    "empty pm model",
			modify:  func(cfg *config.Config) { cfg.Agents.Models.PM = "" },
			wantErr: "model pm",
		},
		{
			name:    "empty engineer model",
			modify:  func(cfg *config.Config) { cfg.Agents.Models.Engineer = "" },
			wantErr: "model engineer",
		},
		{
			name:    "coverage too high",
			modify:  func(cfg *config.Config) { cfg.Quality.CoverageThreshold = 101 },
			wantErr: "coverage_threshold",
		},
		{
			name:    "coverage negative",
			modify:  func(cfg *config.Config) { cfg.Quality.CoverageThreshold = -1 },
			wantErr: "coverage_threshold",
		},
		{
			name:    "zero retro_after_tickets",
			modify:  func(cfg *config.Config) { cfg.Rituals.RetroAfterTickets = 0 },
			wantErr: "retro_after_tickets",
		},
		{
			name:    "unsupported embeddings provider",
			modify:  func(cfg *config.Config) { cfg.Embeddings.Provider = "openai" },
			wantErr: "embeddings provider",
		},
		{
			name:    "empty worktree base_dir",
			modify:  func(cfg *config.Config) { cfg.Worktree.BaseDir = "" },
			wantErr: "base_dir",
		},
		{
			name:    "missing commands channel",
			modify:  func(cfg *config.Config) { cfg.Slack.Channels = []string{"feed", "engineering"} },
			wantErr: "commands",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := config.DefaultConfig()
			tt.modify(&cfg)

			err := cfg.Validate()

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestValidate_ValidConfig_ReturnsNil(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()

	err := cfg.Validate()

	assert.NoError(t, err)
}

func TestDefaultConfig_AllFieldsPopulated(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()

	assert.NotEmpty(t, cfg.Project.Name)
	assert.NotEmpty(t, cfg.Slack.BotTokenEnv)
	assert.NotEmpty(t, cfg.Slack.AppTokenEnv)
	assert.NotEmpty(t, cfg.Slack.Channels)
	assert.NotEmpty(t, cfg.Embeddings.Provider)
	assert.NotEmpty(t, cfg.Embeddings.Model)
	assert.NotEmpty(t, cfg.Embeddings.OllamaURL)
	assert.Greater(t, cfg.Agents.MaxParallel, 0)
	assert.Greater(t, cfg.Agents.CooldownSeconds, 0)
	assert.Greater(t, cfg.Agents.TicketBatchSize, 0)
	assert.Greater(t, cfg.Agents.PersonalityRegenEvery, 0)
	assert.NotEmpty(t, cfg.Agents.Models.PM)
	assert.NotEmpty(t, cfg.Agents.Models.TechLead)
	assert.NotEmpty(t, cfg.Agents.Models.Engineer)
	assert.NotEmpty(t, cfg.Agents.Models.Reviewer)
	assert.NotEmpty(t, cfg.Agents.Models.Designer)
	assert.NotEmpty(t, cfg.Quality.Lint)
	assert.NotEmpty(t, cfg.Quality.Test)
	assert.Greater(t, cfg.Quality.CoverageThreshold, 0)
	assert.NotEmpty(t, cfg.Rituals.StandupCron)
	assert.Greater(t, cfg.Rituals.RetroAfterTickets, 0)
	assert.True(t, cfg.Rituals.DesignDiscussionOnEpics)
	assert.NotEmpty(t, cfg.Worktree.BaseDir)
	assert.True(t, cfg.Worktree.AutoCleanup)
}

func writeTestTOML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "squad0.toml")
	err := os.WriteFile(path, []byte(content), 0o644)
	require.NoError(t, err)
	return path
}

const validTOML = `
[project]
name = "squad0"
repo = "github.com/JR-G/squad0"

[slack]
bot_token_env = "SLACK_BOT_TOKEN"
app_token_env = "SLACK_APP_TOKEN"
channels = ["feed", "engineering", "reviews", "triage", "standup", "commands"]

[github]
owner = "JR-G"

[embeddings]
provider = "ollama"
model = "nomic-embed-text"
ollama_url = "http://localhost:11434"

[agents]
max_parallel = 3
cooldown_seconds = 300
ticket_batch_size = 3
personality_regen_every = 20

[agents.models]
pm = "claude-haiku-4-5-20251001"
tech_lead = "claude-opus-4-6"
engineer = "claude-sonnet-4-6"
reviewer = "claude-opus-4-6"
designer = "claude-sonnet-4-6"

[quality]
lint = "golangci-lint run"
test = "go test -race ./..."
coverage_threshold = 80

[rituals]
standup_cron = "0 9 * * *"
retro_after_tickets = 10
design_discussion_on_epics = true

[worktree]
base_dir = ".worktrees"
auto_cleanup = true
`
