package config

// DefaultConfig returns a Config populated with default values for all fields.
func DefaultConfig() Config {
	return Config{
		Project: ProjectConfig{
			Name: "squad0",
		},
		Linear: LinearConfig{},
		Slack: SlackConfig{
			BotTokenEnv: "SLACK_BOT_TOKEN",
			AppTokenEnv: "SLACK_APP_TOKEN",
			Channels: map[string]string{
				"feed":        "",
				"engineering": "",
				"reviews":     "",
				"triage":      "",
				"standup":     "",
				"commands":    "",
			},
		},
		GitHub: GitHubConfig{},
		Embeddings: EmbeddingsConfig{
			Provider:  "ollama",
			Model:     "nomic-embed-text",
			OllamaURL: "http://localhost:11434",
		},
		Agents: AgentsConfig{
			MaxParallel:           3,
			CooldownSeconds:       300,
			TicketBatchSize:       3,
			PersonalityRegenEvery: 20,
			Models: ModelsConfig{
				PM:       "claude-haiku-4-5-20251001",
				TechLead: "claude-opus-4-6",
				Engineer: "claude-sonnet-4-6",
				Reviewer: "claude-opus-4-6",
				Designer: "claude-sonnet-4-6",
			},
			Runtime: RuntimeConfig{
				Default:  "claude",
				Fallback: "codex",
			},
			Budget: BudgetConfig{
				MaxTokensPerTicket:   0, // 0 = no limit
				MaxTokensPerAgentDay: 0,
			},
		},
		Quality: QualityConfig{
			Lint:              "golangci-lint run",
			Test:              "go test -race ./...",
			CoverageThreshold: 95,
		},
		Rituals: RitualsConfig{
			StandupCron:             "0 9 * * *",
			RetroAfterTickets:       10,
			DesignDiscussionOnEpics: true,
		},
		Worktree: WorktreeConfig{
			BaseDir:     ".worktrees",
			AutoCleanup: true,
		},
	}
}
