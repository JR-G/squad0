package orchestrator

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/health"
	slack "github.com/JR-G/squad0/internal/integrations/slack"
)

// Scheduler runs periodic rituals — standups, retros, and health checks.
type Scheduler struct {
	bot             *slack.Bot
	monitor         *health.Monitor
	alerter         *health.Alerter
	standupInterval time.Duration
	healthInterval  time.Duration
	completedCount  int
	retroThreshold  int
}

// SchedulerConfig holds configuration for ritual scheduling.
type SchedulerConfig struct {
	StandupInterval   time.Duration
	HealthInterval    time.Duration
	RetroAfterTickets int
}

// NewScheduler creates a Scheduler with the given dependencies.
func NewScheduler(
	bot *slack.Bot,
	monitor *health.Monitor,
	alerter *health.Alerter,
	cfg SchedulerConfig,
) *Scheduler {
	return &Scheduler{
		bot:             bot,
		monitor:         monitor,
		alerter:         alerter,
		standupInterval: cfg.StandupInterval,
		healthInterval:  cfg.HealthInterval,
		retroThreshold:  cfg.RetroAfterTickets,
	}
}

// Run starts the ritual scheduling loop. Blocks until context is cancelled.
func (sched *Scheduler) Run(ctx context.Context) error {
	standupTicker := time.NewTicker(sched.standupInterval)
	healthTicker := time.NewTicker(sched.healthInterval)
	defer standupTicker.Stop()
	defer healthTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-standupTicker.C:
			sched.runStandup(ctx)
		case <-healthTicker.C:
			sched.runHealthCheck(ctx)
		}
	}
}

// RecordCompletion increments the completed ticket counter and triggers
// a retro if the threshold is reached.
func (sched *Scheduler) RecordCompletion(ctx context.Context) {
	sched.completedCount++

	if sched.completedCount < sched.retroThreshold {
		return
	}

	sched.completedCount = 0
	sched.runRetro(ctx)
}

// CompletedCount returns the number of completed tickets since the last
// retro.
func (sched *Scheduler) CompletedCount() int {
	return sched.completedCount
}

func (sched *Scheduler) runStandup(ctx context.Context) {
	log.Println("running standup")

	healthStates := sched.monitor.AllHealth(ctx)
	summary := health.FormatHealthSummary(healthStates)

	if sched.bot == nil {
		return
	}

	_ = sched.bot.PostAsRole(ctx, "standup", summary, agent.RolePM)
}

func (sched *Scheduler) runHealthCheck(ctx context.Context) {
	if sched.alerter == nil {
		return
	}

	alertCount, err := sched.alerter.CheckAndAlert(ctx)
	if err != nil {
		log.Printf("health check error: %v", err)
		return
	}

	if alertCount > 0 {
		log.Printf("posted %d health alerts", alertCount)
	}
}

func (sched *Scheduler) runRetro(ctx context.Context) {
	log.Println("running retro")

	msg := fmt.Sprintf("*Retro* — %d tickets completed since last retro. Time to reflect on what went well and what to improve.", sched.retroThreshold)

	if sched.bot == nil {
		return
	}

	_ = sched.bot.PostAsRole(ctx, "feed", msg, agent.RolePM)
}
