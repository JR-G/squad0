package pipeline

import (
	"time"

	"github.com/JR-G/squad0/internal/agent"
)

// AgentStats summarises an agent's performance across completed work items.
type AgentStats struct {
	Agent           agent.Role
	Completed       int
	Failed          int
	AvgDuration     time.Duration
	AvgReviewCycles float64
}

// ComputeStats calculates performance metrics from a set of work items.
// Items should all belong to the same agent.
func ComputeStats(role agent.Role, items []WorkItem) AgentStats {
	stats := AgentStats{Agent: role}

	var totalDuration time.Duration
	var totalCycles int

	for _, item := range items {
		switch {
		case item.Stage == StageMerged:
			stats.Completed++
		case item.Stage == StageFailed:
			stats.Failed++
		default:
			continue
		}

		if item.FinishedAt != nil {
			totalDuration += item.FinishedAt.Sub(item.StartedAt)
		}

		totalCycles += item.ReviewCycles
	}

	terminal := stats.Completed + stats.Failed
	if terminal == 0 {
		return stats
	}

	stats.AvgDuration = totalDuration / time.Duration(terminal)
	stats.AvgReviewCycles = float64(totalCycles) / float64(terminal)

	return stats
}
