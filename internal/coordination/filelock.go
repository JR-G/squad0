package coordination

import (
	"context"

	"github.com/JR-G/squad0/internal/agent"
)

// FileConflict describes a file that two agents both want to touch.
type FileConflict struct {
	File        string
	HeldBy      agent.Role
	RequestedBy agent.Role
}

// CheckFileConflicts compares the planned files for an agent against all
// other agents' current check-ins and returns any conflicts.
func CheckFileConflicts(ctx context.Context, store *CheckInStore, role agent.Role, plannedFiles []string) ([]FileConflict, error) {
	allCheckIns, err := store.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	plannedSet := make(map[string]bool, len(plannedFiles))
	for _, file := range plannedFiles {
		plannedSet[file] = true
	}

	var conflicts []FileConflict
	for _, checkIn := range allCheckIns {
		if checkIn.Agent == role {
			continue
		}

		if checkIn.Status == StatusIdle {
			continue
		}

		for _, file := range checkIn.FilesTouching {
			if !plannedSet[file] {
				continue
			}

			conflicts = append(conflicts, FileConflict{
				File:        file,
				HeldBy:      checkIn.Agent,
				RequestedBy: role,
			})
		}
	}

	return conflicts, nil
}
