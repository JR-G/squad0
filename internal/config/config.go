package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Config represents the complete squad0 configuration loaded from TOML.
type Config struct {
	Project    ProjectConfig    `toml:"project"`
	Linear     LinearConfig     `toml:"linear"`
	Slack      SlackConfig      `toml:"slack"`
	GitHub     GitHubConfig     `toml:"github"`
	Embeddings EmbeddingsConfig `toml:"embeddings"`
	Agents     AgentsConfig     `toml:"agents"`
	Quality    QualityConfig    `toml:"quality"`
	Rituals    RitualsConfig    `toml:"rituals"`
	Worktree   WorktreeConfig   `toml:"worktree"`
}

// ProjectConfig holds project identification settings.
type ProjectConfig struct {
	Name       string `toml:"name"`
	Repo       string `toml:"repo"`
	TargetRepo string `toml:"target_repo"`
}

// LinearConfig holds Linear integration context passed to agent prompts.
type LinearConfig struct {
	TeamID    string `toml:"team_id"`
	ProjectID string `toml:"project_id"`
	Workspace string `toml:"workspace"`
}

// SlackConfig holds Slack integration settings.
type SlackConfig struct {
	BotTokenEnv string            `toml:"bot_token_env"`
	AppTokenEnv string            `toml:"app_token_env"`
	Channels    map[string]string `toml:"channels"`
}

// GitHubConfig holds GitHub integration context passed to agent prompts.
type GitHubConfig struct {
	Owner string `toml:"owner"`
}

// EmbeddingsConfig holds embedding provider settings.
type EmbeddingsConfig struct {
	Provider  string `toml:"provider"`
	Model     string `toml:"model"`
	OllamaURL string `toml:"ollama_url"`
}

// AgentsConfig holds agent orchestration settings.
type AgentsConfig struct {
	MaxParallel           int          `toml:"max_parallel"`
	CooldownSeconds       int          `toml:"cooldown_seconds"`
	TicketBatchSize       int          `toml:"ticket_batch_size"`
	PersonalityRegenEvery int          `toml:"personality_regen_every"`
	CodexFallbackModel    string       `toml:"codex_fallback_model"`
	Models                ModelsConfig `toml:"models"`
}

// ModelsConfig maps agent roles to Claude model identifiers.
type ModelsConfig struct {
	PM       string `toml:"pm"`
	TechLead string `toml:"tech_lead"`
	Engineer string `toml:"engineer"`
	Reviewer string `toml:"reviewer"`
	Designer string `toml:"designer"`
}

// QualityConfig holds quality gate settings.
type QualityConfig struct {
	Lint              string `toml:"lint"`
	Test              string `toml:"test"`
	CoverageThreshold int    `toml:"coverage_threshold"`
}

// RitualsConfig holds scheduling settings for team rituals.
type RitualsConfig struct {
	StandupCron             string `toml:"standup_cron"`
	RetroAfterTickets       int    `toml:"retro_after_tickets"`
	DesignDiscussionOnEpics bool   `toml:"design_discussion_on_epics"`
}

// WorktreeConfig holds git worktree management settings.
type WorktreeConfig struct {
	BaseDir     string `toml:"base_dir"`
	AutoCleanup bool   `toml:"auto_cleanup"`
}

// Load reads the TOML configuration file at the given path and returns
// a validated Config. Fields not present in the file use default values.
func Load(path string) (Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("reading config file: %w", err)
	}

	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parsing config file: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validating config: %w", err)
	}

	return cfg, nil
}

// Validate checks the Config for internal consistency and returns the first
// error found, or nil if the configuration is valid.
func (cfg Config) Validate() error {
	if cfg.Project.Name == "" {
		return fmt.Errorf("project name must not be empty")
	}

	if cfg.Agents.MaxParallel < 1 || cfg.Agents.MaxParallel > 10 {
		return fmt.Errorf("agents max_parallel must be between 1 and 10, got %d", cfg.Agents.MaxParallel)
	}

	if cfg.Agents.CooldownSeconds < 0 {
		return fmt.Errorf("agents cooldown_seconds must be non-negative, got %d", cfg.Agents.CooldownSeconds)
	}

	if cfg.Agents.TicketBatchSize < 1 {
		return fmt.Errorf("agents ticket_batch_size must be at least 1, got %d", cfg.Agents.TicketBatchSize)
	}

	if cfg.Agents.PersonalityRegenEvery < 1 {
		return fmt.Errorf("agents personality_regen_every must be at least 1, got %d", cfg.Agents.PersonalityRegenEvery)
	}

	if err := validateModels(cfg.Agents.Models); err != nil {
		return err
	}

	if cfg.Quality.CoverageThreshold < 0 || cfg.Quality.CoverageThreshold > 100 {
		return fmt.Errorf("quality coverage_threshold must be between 0 and 100, got %d", cfg.Quality.CoverageThreshold)
	}

	if cfg.Rituals.RetroAfterTickets < 1 {
		return fmt.Errorf("rituals retro_after_tickets must be at least 1, got %d", cfg.Rituals.RetroAfterTickets)
	}

	if cfg.Embeddings.Provider != "ollama" {
		return fmt.Errorf("embeddings provider must be \"ollama\", got %q", cfg.Embeddings.Provider)
	}

	if cfg.Worktree.BaseDir == "" {
		return fmt.Errorf("worktree base_dir must not be empty")
	}

	if _, hasCommands := cfg.Slack.Channels["commands"]; !hasCommands {
		return fmt.Errorf("slack channels must include \"commands\"")
	}

	return nil
}

func validateModels(models ModelsConfig) error {
	checks := []struct {
		name  string
		value string
	}{
		{"pm", models.PM},
		{"tech_lead", models.TechLead},
		{"engineer", models.Engineer},
		{"reviewer", models.Reviewer},
		{"designer", models.Designer},
	}

	for _, check := range checks {
		if check.value == "" {
			return fmt.Errorf("agents model %s must not be empty", check.name)
		}
	}

	return nil
}
