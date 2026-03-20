package orchestrator_test

import (
	"context"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/health"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
)

func newTestScheduler() *orchestrator.Scheduler {
	roles := []agent.Role{agent.RoleEngineer1, agent.RoleEngineer2}
	monitor := health.NewMonitor(roles, health.MonitorConfig{
		MaxSessionTime:       30 * time.Minute,
		MaxConsecutiveErrors: 3,
	})

	return orchestrator.NewScheduler(nil, monitor, nil, orchestrator.SchedulerConfig{
		StandupInterval:   time.Hour,
		HealthInterval:    5 * time.Minute,
		RetroAfterTickets: 3,
	})
}

func TestScheduler_RecordCompletion_IncrementsCount(t *testing.T) {
	t.Parallel()

	sched := newTestScheduler()

	sched.RecordCompletion(context.Background())
	sched.RecordCompletion(context.Background())

	assert.Equal(t, 2, sched.CompletedCount())
}

func TestScheduler_RecordCompletion_TriggersRetroAtThreshold(t *testing.T) {
	t.Parallel()

	sched := newTestScheduler()
	ctx := context.Background()

	sched.RecordCompletion(ctx)
	sched.RecordCompletion(ctx)
	assert.Equal(t, 2, sched.CompletedCount())

	sched.RecordCompletion(ctx)
	assert.Equal(t, 0, sched.CompletedCount())
}

func TestScheduler_Run_StopsOnContextCancel(t *testing.T) {
	t.Parallel()

	sched := newTestScheduler()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := sched.Run(ctx)

	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestScheduler_CompletedCount_StartsAtZero(t *testing.T) {
	t.Parallel()

	sched := newTestScheduler()

	assert.Equal(t, 0, sched.CompletedCount())
}
