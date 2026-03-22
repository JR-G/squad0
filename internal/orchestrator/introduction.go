package orchestrator

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
	slack "github.com/JR-G/squad0/internal/integrations/slack"
)

const introductionPrompt = `This is your very first session. Welcome to the team.

Your first task: choose your name. Pick a name that feels right for who you are — something that reflects your personality and how you work. This will be your permanent identity.

Then introduce yourself to the team. Tell them:
- Your chosen name
- What kind of work you're drawn to
- How you approach problems

Keep it natural and brief — a few sentences. Start with "My name is [your chosen name]."

Important: respond with ONLY your introduction. No code, no markdown, no analysis. Just your introduction as plain text.`

// RunIntroductions spawns a short session for each agent that hasn't
// chosen a name yet. The agent picks their name and introduces
// themselves in #feed.
func RunIntroductions(
	ctx context.Context,
	agents map[agent.Role]*agent.Agent,
	personaStore *slack.PersonaStore,
	bot *slack.Bot,
) {
	for _, role := range agent.AllRoles() {
		agentInstance, ok := agents[role]
		if !ok {
			continue
		}

		if personaStore.HasChosenName(ctx, role) {
			log.Printf("agent %s already has a name, skipping introduction", role)
			continue
		}

		runSingleIntroduction(ctx, role, agentInstance, personaStore, bot)
	}
}

func runSingleIntroduction(
	ctx context.Context,
	role agent.Role,
	agentInstance *agent.Agent,
	personaStore *slack.PersonaStore,
	bot *slack.Bot,
) {
	log.Printf("running introduction for %s", role)

	result, err := agentInstance.ExecuteTask(ctx, introductionPrompt, nil, "")
	if err != nil {
		log.Printf("introduction failed for %s: %v", role, err)
		return
	}

	chosenName := ExtractName(result.Transcript)
	if chosenName == "" {
		log.Printf("could not extract name for %s from transcript", role)
		return
	}

	if err := personaStore.SaveChosenName(ctx, role, chosenName); err != nil {
		log.Printf("failed to save name for %s: %v", role, err)
		return
	}

	log.Printf("agent %s chose name: %s", role, chosenName)

	introduction := strings.TrimSpace(result.Transcript)
	if introduction == "" {
		introduction = fmt.Sprintf("Hi, I'm %s. Looking forward to working with the team.", chosenName)
	}

	postIntroduction(ctx, bot, personaStore, role, introduction)
}

func postIntroduction(ctx context.Context, bot *slack.Bot, personaStore *slack.PersonaStore, role agent.Role, introduction string) {
	if bot == nil {
		return
	}

	persona := personaStore.LoadPersona(ctx, role)
	err := bot.PostMessage(ctx, "feed", introduction, persona)
	if err != nil {
		log.Printf("failed to post introduction for %s: %v", role, err)
	}

	bot.UpdatePersonas(personaStore.LoadAllPersonas(ctx))
}

// ExtractName parses an agent's chosen name from their introduction
// transcript by looking for common patterns like "My name is X".
func ExtractName(transcript string) string {
	lower := strings.ToLower(transcript)

	prefixes := []string{
		"my name is ",
		"i'm ",
		"i am ",
		"call me ",
		"i've chosen the name ",
		"i chose the name ",
		"i'll go by ",
	}

	for _, prefix := range prefixes {
		idx := strings.Index(lower, prefix)
		if idx == -1 {
			continue
		}

		start := idx + len(prefix)
		rest := transcript[start:]

		name := extractFirstWord(rest)
		name = strings.Trim(name, ".,!?;:'\"")

		if name != "" {
			return name
		}
	}

	return ""
}

func extractFirstWord(text string) string {
	text = strings.TrimSpace(text)
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return ""
	}

	return fields[0]
}
