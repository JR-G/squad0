package pipeline

// Stage represents a work item's position in the engineering pipeline.
type Stage string

const (
	// StageAssigned means the ticket has been assigned to an engineer.
	StageAssigned Stage = "assigned"
	// StageWorking means the engineer is actively implementing.
	StageWorking Stage = "working"
	// StagePROpened means a pull request has been created.
	StagePROpened Stage = "pr_opened"
	// StageReviewing means the reviewer is examining the PR.
	StageReviewing Stage = "reviewing"
	// StageChangesRequested means the reviewer asked for fixes.
	StageChangesRequested Stage = "changes_requested"
	// StageApproved means the PR passed review.
	StageApproved Stage = "approved"
	// StageMerged means the PR has been merged.
	StageMerged Stage = "merged"
	// StageFailed means the work item was abandoned.
	StageFailed Stage = "failed"
)

// IsTerminal returns true if the stage represents a completed work item.
func (stage Stage) IsTerminal() bool {
	return stage == StageMerged || stage == StageFailed
}
