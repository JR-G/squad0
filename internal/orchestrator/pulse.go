package orchestrator

import (
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
	slack "github.com/JR-G/squad0/internal/integrations/slack"
)

const pmBriefingPrompt = `You just came online with your team. Everyone has introduced themselves in #feed.

Post a message to the engineering channel. Welcome the team, mention what you're going to focus on (checking the board, looking at priorities), and set the tone. Be natural — talk like a real PM, not a bot.

Keep it brief. No markdown, no bullet points. Just talk.`

// RunPMBriefing has the PM post an initial team update.
func RunPMBriefing(ctx context.Context, agents map[agent.Role]*agent.Agent, bot *slack.Bot) {
	pmAgent, ok := agents[agent.RolePM]
	if !ok {
		return
	}

	log.Println("PM posting initial briefing")

	result, err := pmAgent.ExecuteTask(ctx, pmBriefingPrompt, nil, "")
	if err != nil {
		log.Printf("PM briefing failed: %v", err)
		return
	}

	postToEngineering(ctx, bot, result.Transcript, agent.RolePM)
}

// RunConversationRound gives a few agents the chance to respond to
// recent activity in #engineering. Used after the PM briefing to
// kickstart natural conversation.
func RunConversationRound(ctx context.Context, agents map[agent.Role]*agent.Agent, bot *slack.Bot) {
	allRoles := agent.AllRoles()
	responded := make(map[agent.Role]bool)
	var recentMessages []string

	maxResponders := 3

	for range maxResponders {
		candidates := make([]agent.Role, 0, len(allRoles))
		for _, role := range allRoles {
			if role == agent.RolePM || role == agent.RoleReviewer || responded[role] {
				continue
			}
			candidates = append(candidates, role)
		}

		if len(candidates) == 0 {
			break
		}

		role := candidates[rand.IntN(len(candidates))]
		agentInstance, ok := agents[role]
		if !ok {
			continue
		}

		prompt := buildPulsePrompt(recentMessages)
		result, err := agentInstance.ExecuteTask(ctx, prompt, nil, "")
		if err != nil {
			log.Printf("conversation round failed for %s: %v", role, err)
			continue
		}

		text := strings.TrimSpace(result.Transcript)
		if text == "" {
			responded[role] = true
			continue
		}

		if postToEngineering(ctx, bot, text, role) {
			recentMessages = append(recentMessages, fmt.Sprintf("%s: %s", role, text))
			responded[role] = true
		}
	}
}

// RunIdlePulse picks a random idle agent and gives them recent channel
// context so they can respond naturally. Returns true if someone posted.
func RunIdlePulse(ctx context.Context, agents map[agent.Role]*agent.Agent, idleRoles []agent.Role, bot *slack.Bot, recentMessages []string) bool {
	candidates := filterChattyRoles(idleRoles)
	if len(candidates) == 0 {
		return false
	}

	role := candidates[rand.IntN(len(candidates))]
	agentInstance, ok := agents[role]
	if !ok {
		return false
	}

	prompt := buildPulsePrompt(recentMessages)

	log.Printf("idle pulse for %s", role)

	result, err := agentInstance.ExecuteTask(ctx, prompt, nil, "")
	if err != nil {
		log.Printf("idle pulse failed for %s: %v", role, err)
		return false
	}

	return postToEngineering(ctx, bot, result.Transcript, role)
}

func buildPulsePrompt(recentMessages []string) string {
	var builder strings.Builder

	builder.WriteString("Here's what's been said recently in the engineering channel:\n\n")

	if len(recentMessages) == 0 {
		builder.WriteString("(nothing yet — the channel is quiet)\n")
	}

	for _, msg := range recentMessages {
		fmt.Fprintf(&builder, "> %s\n", msg)
	}

	builder.WriteString("\nIf you have something to add — a response, a thought, a question — post it. ")
	builder.WriteString("If you don't have anything meaningful to say, just respond with exactly: PASS\n\n")
	builder.WriteString("Keep it natural and brief. No markdown. Just talk.")

	return builder.String()
}

func filterChattyRoles(roles []agent.Role) []agent.Role {
	chatty := map[agent.Role]bool{
		agent.RolePM:        true,
		agent.RoleTechLead:  true,
		agent.RoleEngineer1: true,
		agent.RoleEngineer2: true,
		agent.RoleEngineer3: true,
		agent.RoleDesigner:  true,
	}

	result := make([]agent.Role, 0, len(roles))
	for _, role := range roles {
		if chatty[role] {
			result = append(result, role)
		}
	}

	return result
}

func postToEngineering(ctx context.Context, bot *slack.Bot, transcript string, role agent.Role) bool {
	text := strings.TrimSpace(transcript)

	if text == "" {
		return false
	}

	if bot == nil {
		return false
	}

	err := bot.PostAsRole(ctx, "engineering", text, role)
	if err != nil {
		log.Printf("failed to post for %s: %v", role, err)
		return false
	}

	return true
}
