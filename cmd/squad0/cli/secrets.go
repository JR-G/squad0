package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/JR-G/squad0/internal/secrets"
	"github.com/spf13/cobra"
)

func newSecretsCommand() *cobra.Command {
	secretsCmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage secrets stored in macOS Keychain",
	}

	secretsCmd.AddCommand(
		newSecretsSetCommand(),
		newSecretsListCommand(),
		newSecretsVerifyCommand(),
	)

	return secretsCmd
}

func newSecretsSetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set <name>",
		Short: "Store a secret in macOS Keychain",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			ctx := context.Background()
			mgr := newSecretManager()

			fmt.Fprintf(os.Stderr, "Enter value for %s: ", name)

			reader := bufio.NewReader(os.Stdin)
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

			fmt.Fprintf(os.Stdout, "Secret %s stored successfully.\n", name)
			return nil
		},
	}
}

func newSecretsListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show which secrets are configured (names only, never values)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			mgr := newSecretManager()

			status, err := mgr.List(ctx)
			if err != nil {
				return err
			}

			for _, name := range secrets.RequiredSecrets {
				label := "[not set]"
				if status[name] {
					label = "[set]"
				}
				fmt.Fprintf(os.Stdout, "  %-20s %s\n", name, label)
			}

			return nil
		},
	}
}

func newSecretsVerifyCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Check all required secrets are present",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			mgr := newSecretManager()

			_, err := mgr.Verify(ctx)
			if err != nil {
				return err
			}

			fmt.Fprintln(os.Stdout, "All required secrets are configured.")
			return nil
		},
	}
}

func newSecretManager() *secrets.Manager {
	runner := secrets.ExecRunner{}
	kc := secrets.NewKeychain(secrets.ServiceName, runner)
	return secrets.NewManager(kc)
}
