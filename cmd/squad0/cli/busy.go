package cli

import (
	"context"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/orchestrator"
)

// busyCheckerFromCheckIns returns a BusyChecker that reports an agent
// as busy when their coordination check-in is in the working state.
// Used by the conversation engine to keep engineers heads-down while
// they implement a ticket — they won't be picked for chat replies
// until the pipeline transitions them back to idle.
func busyCheckerFromCheckIns(store *coordination.CheckInStore) orchestrator.BusyChecker {
	return func(ctx context.Context, role agent.Role) bool {
		checkIn, err := store.GetByAgent(ctx, role)
		if err != nil {
			return false
		}
		return checkIn.Status == coordination.StatusWorking
	}
}
