package cli

import (
	"fmt"
	"os"

	"github.com/JR-G/squad0/internal/config"
	"github.com/spf13/cobra"
)

const version = "0.1.0"

// NewRootCommand creates the top-level cobra command for squad0 with all
// subcommands registered.
func NewRootCommand() *cobra.Command {
	var configPath string

	rootCmd := &cobra.Command{
		Use:   "squad0",
		Short: "Autonomous software engineering team orchestrator",
	}

	rootCmd.PersistentFlags().StringVar(&configPath, "config", "config/squad0.toml",
		"path to the configuration file")

	rootCmd.AddCommand(
		newConfigCommand(&configPath),
		newSecretsCommand(),
		newVersionCommand(),
	)

	return rootCmd
}

func newConfigCommand(configPath *string) *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Configuration management commands",
	}

	validateCmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate the configuration file",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := config.Load(*configPath)
			if err != nil {
				return fmt.Errorf("configuration invalid: %w", err)
			}

			fmt.Fprintln(os.Stdout, "Configuration valid.")
			return nil
		},
	}

	configCmd.AddCommand(validateCmd)
	return configCmd
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show the squad0 version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "squad0 version %s\n", version)
		},
	}
}
