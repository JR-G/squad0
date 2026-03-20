package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/JR-G/squad0/internal/secrets"
	"github.com/spf13/cobra"
)

// SecretsCommandDeps holds injectable dependencies for secrets commands.
type SecretsCommandDeps struct {
	Manager *secrets.Manager
	Stdin   io.Reader
}

func newSecretsCommand() *cobra.Command {
	return newSecretsCommandWith(nil)
}

func newSecretsCommandWith(deps *SecretsCommandDeps) *cobra.Command {
	secretsCmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage secrets stored in macOS Keychain",
	}

	secretsCmd.AddCommand(
		newSecretsSetCommand(deps),
		newSecretsListCommand(deps),
		newSecretsVerifyCommand(deps),
	)

	return secretsCmd
}

func resolveManager(deps *SecretsCommandDeps) *secrets.Manager {
	if deps != nil && deps.Manager != nil {
		return deps.Manager
	}
	runner := secrets.ExecRunner{}
	kc := secrets.NewKeychain(secrets.ServiceName, runner)
	return secrets.NewManager(kc)
}

func resolveStdin(deps *SecretsCommandDeps) io.Reader {
	if deps != nil && deps.Stdin != nil {
		return deps.Stdin
	}
	return nil
}

func newSecretsSetCommand(deps *SecretsCommandDeps) *cobra.Command {
	return &cobra.Command{
		Use:   "set <name>",
		Short: "Store a secret in macOS Keychain",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			ctx := context.Background()
			mgr := resolveManager(deps)

			_, err := fmt.Fprintf(cmd.ErrOrStderr(), "Enter value for %s: ", name)
			if err != nil {
				return fmt.Errorf("writing prompt: %w", err)
			}

			input := resolveStdin(deps)
			if input == nil {
				input = cmd.InOrStdin()
			}

			reader := bufio.NewReader(input)
			value, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("reading input: %w", err)
			}

			value = strings.TrimSpace(value)
			if value == "" {
				return fmt.Errorf("secret value must not be empty")
			}

			if err := mgr.Set(ctx, name, value); err != nil {
				return err
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Secret %s stored successfully.\n", name)
			return err
		},
	}
}

func newSecretsListCommand(deps *SecretsCommandDeps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show which secrets are configured (names only, never values)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			mgr := resolveManager(deps)

			status, err := mgr.List(ctx)
			if err != nil {
				return err
			}

			for _, name := range secrets.RequiredSecrets {
				label := "[not set]"
				if status[name] {
					label = "[set]"
				}
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  %-20s %s\n", name, label); err != nil {
					return err
				}
			}

			return nil
		},
	}
}

func newSecretsVerifyCommand(deps *SecretsCommandDeps) *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Check all required secrets are present",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			mgr := resolveManager(deps)

			_, err := mgr.Verify(ctx)
			if err != nil {
				return err
			}

			_, err = fmt.Fprintln(cmd.OutOrStdout(), "All required secrets are configured.")
			return err
		},
	}
}
