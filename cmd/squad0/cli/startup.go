package cli

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/config"
	slack "github.com/JR-G/squad0/internal/integrations/slack"
	"github.com/JR-G/squad0/internal/orchestrator"
)

// parseCronToInterval parses a cron expression of the form "M H * * *"
// and returns the duration until the next occurrence. Only the hour
// field is used; unsupported expressions fall back to 24h.
func parseCronToInterval(cron string) time.Duration {
	parts := strings.Fields(cron)
	if len(parts) < 2 {
		return 24 * time.Hour
	}

	hour, err := strconv.Atoi(parts[1])
	if err != nil || hour < 0 || hour > 23 {
		return 24 * time.Hour
	}

	return durationUntilHour(hour, time.Now())
}

// durationUntilHour returns the duration from now until the next
// occurrence of the given hour (in local time). If the hour has already
// passed today, it returns the duration until that hour tomorrow.
func durationUntilHour(hour int, now time.Time) time.Duration {
	target := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, now.Location())
	if !target.After(now) {
		target = target.Add(24 * time.Hour)
	}
	return target.Sub(now)
}

// assertMemoryStoresWired fails startup if any agent is missing the
// stores required by the post-session memory flush. Without this, a
// constructor mistake silently drops learnings on every session and
// the only visible symptom is degraded recall weeks later.
func assertMemoryStoresWired(agents map[agent.Role]*agent.Agent) error {
	var missing []string
	for role, instance := range agents {
		if instance == nil || !instance.HasMemoryStores() {
			missing = append(missing, string(role))
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("agents missing memory stores: %v", missing)
}

// seedConversationHistory loads recent messages from Slack channels
// and seeds them into the conversation engine so agents have context
// about what was discussed before a restart.
func seedConversationHistory(
	ctx context.Context,
	bot *slack.Bot,
	conversation *orchestrator.ConversationEngine,
	_ config.Config,
) {
	if bot == nil || conversation == nil {
		return
	}

	channels := []string{"engineering", "reviews", "feed"}

	for _, channel := range channels {
		messages, err := bot.LoadRecentMessages(ctx, channel, 15)
		if err != nil {
			log.Printf("failed to load history for #%s: %v", channel, err)
			continue
		}

		lines := make([]string, 0, len(messages))
		for _, msg := range messages {
			lines = append(lines, fmt.Sprintf("%s: %s", msg.User, msg.Text))
		}

		conversation.SeedHistory(channel, lines)
	}
}
