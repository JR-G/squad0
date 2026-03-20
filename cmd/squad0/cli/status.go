package cli

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/secrets"
	"github.com/JR-G/squad0/internal/tui"
	_ "github.com/mattn/go-sqlite3" // SQLite driver.
	"github.com/spf13/cobra"
)

func newStatusCommand(_ *string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show agent statuses and system health",
		RunE: func(cmd *cobra.Command, args []string) error {
			return showStatus(cmd)
		},
	}
}

func showStatus(cmd *cobra.Command) error {
	ctx := context.Background()
	out := cmd.OutOrStdout()

	_, _ = fmt.Fprint(out, tui.Banner())

	showSecretStatus(ctx, cmd)
	showAgentStatus(ctx, cmd)

	return nil
}

func showSecretStatus(ctx context.Context, cmd *cobra.Command) {
	runner := secrets.ExecRunner{}
	kc := secrets.NewKeychain(secrets.ServiceName, runner)
	mgr := secrets.NewManager(kc)

	status, err := mgr.List(ctx)
	if err != nil {
		_, _ = fmt.Fprint(cmd.OutOrStdout(), tui.StepFail(fmt.Sprintf("Secrets check failed: %v", err)))
		return
	}

	_, _ = fmt.Fprint(cmd.OutOrStdout(), tui.FormatSecretsList(status))
}

func showAgentStatus(ctx context.Context, cmd *cobra.Command) {
	coordDBPath := "data/coordination.db"
	if _, err := os.Stat(coordDBPath); os.IsNotExist(err) {
		_, _ = fmt.Fprint(cmd.OutOrStdout(), tui.Section("Agents"))
		_, _ = fmt.Fprint(cmd.OutOrStdout(), tui.StepPending("No coordination data yet — run `squad0 start` first"))
		return
	}

	checkIns, err := loadCheckIns(ctx, coordDBPath)
	if err != nil {
		_, _ = fmt.Fprint(cmd.OutOrStdout(), tui.StepFail(err.Error()))
		return
	}

	if len(checkIns) == 0 {
		showEmptyAgentList(cmd)
		return
	}

	_, _ = fmt.Fprint(cmd.OutOrStdout(), tui.FormatAgentStatus(checkIns, nil))
}

func loadCheckIns(ctx context.Context, dbPath string) ([]coordination.CheckIn, error) {
	coordDB, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("coordination DB error: %w", err)
	}
	defer func() { _ = coordDB.Close() }()

	store := coordination.NewCheckInStore(coordDB)
	return store.GetAll(ctx)
}

func showEmptyAgentList(cmd *cobra.Command) {
	_, _ = fmt.Fprint(cmd.OutOrStdout(), tui.Section("Agents"))
	for _, role := range agent.AllRoles() {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s %s\n",
			tui.AgentName.Render(fmt.Sprintf("%-14s", role)),
			tui.StatusIdle.Render("not started"),
		)
	}
}
