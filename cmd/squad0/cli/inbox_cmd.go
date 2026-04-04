package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/JR-G/squad0/internal/runtime"
	"github.com/spf13/cobra"
)

// newInboxCommand creates the `squad0 inbox` subcommand group.
func newInboxCommand() *cobra.Command {
	inboxCmd := &cobra.Command{
		Use:    "inbox",
		Short:  "Agent inbox management (hook handlers)",
		Hidden: true,
	}

	inboxCmd.AddCommand(newInboxDrainCommand())
	return inboxCmd
}

// newInboxDrainCommand creates the `squad0 inbox drain` subcommand.
// Called by the UserPromptSubmit hook on every turn boundary.
// Drains the agent's filesystem inbox and prints messages to stdout
// as <system-reminder> blocks. Claude Code captures hook stdout and
// injects it into the agent's context.
func newInboxDrainCommand() *cobra.Command {
	var role string
	var dataDir string

	cmd := &cobra.Command{
		Use:   "drain",
		Short: "Drain agent inbox and print as system-reminder blocks",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInboxDrain(role, dataDir)
		},
	}

	cmd.Flags().StringVar(&role, "role", "", "agent role (e.g. engineer-1)")
	cmd.Flags().StringVar(&dataDir, "data-dir", "data", "path to data directory")
	_ = cmd.MarkFlagRequired("role")

	return cmd
}

func runInboxDrain(role, dataDir string) error {
	inboxDir := filepath.Join(dataDir, "inbox", role)
	outboxDir := filepath.Join(dataDir, "outbox", role)

	// NewInbox creates directories if they don't exist, so this only
	// fails on permission errors — which we should surface.
	inbox, err := runtime.NewInbox(inboxDir, outboxDir)
	if err != nil {
		return fmt.Errorf("opening inbox for %s: %w", role, err)
	}

	messages, drainErr := inbox.Drain()
	if drainErr != nil {
		return fmt.Errorf("draining inbox for %s: %w", role, drainErr)
	}

	if len(messages) == 0 {
		return nil
	}

	// Print to stdout — Claude Code captures this and injects into
	// the agent's context as system-reminder blocks.
	output := runtime.FormatDrained(messages)
	_, writeErr := fmt.Fprint(os.Stdout, output)
	return writeErr
}
