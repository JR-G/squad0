package cli

import "github.com/spf13/cobra"

// NewRootCommandForTest creates a root command with injectable secrets
// dependencies for testing.
func NewRootCommandForTest(deps *SecretsCommandDeps) *cobra.Command {
	var configPath string

	rootCmd := &cobra.Command{
		Use:   "squad0",
		Short: "Autonomous software engineering team orchestrator",
	}

	rootCmd.PersistentFlags().StringVar(&configPath, "config", "config/squad0.toml",
		"path to the configuration file")

	rootCmd.AddCommand(
		newConfigCommand(&configPath),
		newSecretsCommandWith(deps),
		newVersionCommand(),
	)

	return rootCmd
}
