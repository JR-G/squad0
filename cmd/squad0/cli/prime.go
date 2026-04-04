package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/spf13/cobra"
)

// newPrimeCommand creates the `squad0 prime` subcommand.
// Called by the SessionStart hook to inject personality into a
// persistent Claude Code session. Prints to stdout — Claude Code
// captures hook stdout as system context.
func newPrimeCommand() *cobra.Command {
	var role string

	cmd := &cobra.Command{
		Use:    "prime",
		Short:  "Inject agent personality into a persistent session (hook handler)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPrime(cmd.Context(), role)
		},
	}

	cmd.Flags().StringVar(&role, "role", "", "agent role (e.g. engineer-1)")
	_ = cmd.MarkFlagRequired("role")

	return cmd
}

func runPrime(_ context.Context, roleStr string) error {
	agentRole := agent.Role(roleStr)
	loader := agent.NewPersonalityLoader("agents")

	// Load the full personality from the markdown file.
	personality, err := loader.LoadBase(agentRole)
	if err != nil {
		return fmt.Errorf("loading personality for %s: %w", roleStr, err)
	}

	// Load voice section for the voice description.
	voice := loader.LoadVoice(agentRole)

	// Build the CLAUDE.md-style personality output.
	// For persistent sessions, we output this to stdout so Claude Code
	// ingests it as system context via the SessionStart hook.
	roster := loadRosterFromDB(roleStr)
	beliefs := loadBeliefsFromDB(roleStr)

	output := agent.BuildPersonalityCLAUDEMDForPrime(agentRole, roster, beliefs, voice)

	// Also include the full personality file content.
	output += "\n\n## Full Personality\n\n" + personality

	_, writeErr := fmt.Fprint(os.Stdout, output)
	return writeErr
}

// loadRosterFromDB attempts to load the roster from agent DBs.
// Returns a minimal roster if DBs are unavailable.
func loadRosterFromDB(currentRole string) map[agent.Role]string {
	// For now, return role IDs as names. The full roster with chosen
	// names requires loading persona stores which need DB access.
	// The personality itself is the critical piece — names are secondary.
	roster := make(map[agent.Role]string, len(agent.AllRoles()))
	for _, role := range agent.AllRoles() {
		roster[role] = string(role)
	}
	_ = currentRole
	return roster
}

// loadBeliefsFromDB attempts to load top beliefs for the agent.
// Returns empty if DBs are unavailable.
func loadBeliefsFromDB(roleStr string) []string {
	dbPath := fmt.Sprintf("data/agents/%s.db", roleStr)
	if _, err := os.Stat(dbPath); err != nil {
		return nil
	}

	ctx := context.Background()
	db, err := memory.Open(ctx, dbPath)
	if err != nil {
		return nil
	}
	defer func() { _ = db.Close() }()

	factStore := memory.NewFactStore(db)
	beliefs, err := factStore.TopBeliefs(ctx, 5)
	if err != nil {
		return nil
	}

	result := make([]string, 0, len(beliefs))
	for _, belief := range beliefs {
		result = append(result, belief.Content)
	}
	return result
}
