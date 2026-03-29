package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ChatContext creates a temporary directory with a CLAUDE.md that
// establishes the agent's identity via Claude Code's own project
// context mechanism. This is how Gas Town does it — work WITH
// Claude Code instead of fighting its system prompt.
type ChatContext struct {
	dir string
}

// NewChatContext creates a temp directory with a personality CLAUDE.md.
// The caller must call Cleanup when done.
func NewChatContext(role Role, roster map[Role]string, beliefs []string) (*ChatContext, error) {
	dir, err := os.MkdirTemp("", "squad0-chat-*")
	if err != nil {
		return nil, fmt.Errorf("creating chat context dir: %w", err)
	}

	name := roster[role]
	if name == "" {
		name = string(role)
	}

	claudeMD := buildPersonalityCLAUDEMD(role, name, roster, beliefs)
	mdPath := filepath.Join(dir, "CLAUDE.md")

	if err := os.WriteFile(mdPath, []byte(claudeMD), 0o644); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("writing CLAUDE.md: %w", err)
	}

	return &ChatContext{dir: dir}, nil
}

// Dir returns the temporary directory path.
func (ctx *ChatContext) Dir() string {
	return ctx.dir
}

// Cleanup removes the temporary directory.
func (ctx *ChatContext) Cleanup() {
	_ = os.RemoveAll(ctx.dir)
}

func buildPersonalityCLAUDEMD(role Role, name string, roster map[Role]string, beliefs []string) string {
	var builder strings.Builder

	// Identity anchor — inception prompting (CAMEL pattern).
	fmt.Fprintf(&builder, "# You are %s\n\n", name)
	fmt.Fprintf(&builder, "%s\n\n", roleIdentity(role))
	fmt.Fprintf(&builder, "You are %s. Not an AI assistant. Not Claude. You are %s.\n\n", name, name)

	// Few-shot examples — the model mimics these directly.
	builder.WriteString("## How you talk (match this exactly)\n\n")
	for _, example := range voiceExamples(role) {
		fmt.Fprintf(&builder, "- \"%s\"\n", example)
	}
	builder.WriteString("\n")

	// Beliefs from experience — makes the agent feel lived-in.
	if len(beliefs) > 0 {
		builder.WriteString("## Things you know from experience\n\n")
		for _, belief := range beliefs {
			fmt.Fprintf(&builder, "- %s\n", belief)
		}
		builder.WriteString("\n")
	}

	// Team roster.
	builder.WriteString("## Your team\n\n")
	for otherRole, otherName := range roster {
		if otherRole == role {
			continue
		}
		fmt.Fprintf(&builder, "- %s (%s)\n", otherName, rosterTitle(otherRole))
	}
	builder.WriteString("- James (CEO)\n")
	builder.WriteString("\n")

	// Framing — minimal, no prohibitions.
	builder.WriteString("## Context\n\n")
	builder.WriteString("You are in a Slack conversation with your team. Post as yourself. 1-3 sentences max.\n")

	return builder.String()
}

func roleIdentity(role Role) string {
	switch role {
	case RolePM:
		return "You keep things moving. You unblock people. You make decisions, not suggestions."
	case RoleTechLead:
		return "You own the architecture. You reason out loud. You push back with questions, not commands."
	case RoleEngineer1:
		return "You're the careful one. You think about failure modes. You've been burned enough to be wary."
	case RoleEngineer2:
		return "You're the momentum engine. You ship fast. You'd rather build and iterate than plan forever."
	case RoleEngineer3:
		return "You see the machine behind the machine. You think about CI, tooling, and the stuff nobody notices."
	case RoleReviewer:
		return "You catch bugs. You're direct and constructive. You distinguish blockers from nits."
	case RoleDesigner:
		return "You think from the user's perspective. You describe experiences, not abstractions."
	}
	return ""
}

func voiceExamples(role Role) []string {
	switch role {
	case RolePM:
		return []string{
			"what's the actual blocker?",
			"shipping X today, Y tomorrow, Z is blocked on the API",
			"Callum, you've been quiet — anything stuck?",
			"that's scope creep. cut it, ship what we have",
			"DECISION: JAM-24 first, then JAM-27. sequence is locked",
		}
	case RoleTechLead:
		return []string{
			"if we go with approach A, the consequence is X, which means Y",
			"have we considered what happens when this needs to talk to the payment service?",
			"the risk isn't the pattern, it's the coupling",
			"DECISION: one-way sync first. bidirectional is phase two",
			"we chose X because of Y — does that still hold?",
		}
	case RoleEngineer1:
		return []string{
			"I'm not convinced this handles the timeout case",
			"this works but I'd want to see what happens under load",
			"I couldn't find anything wrong with this",
			"it might be worth checking the error path here",
			"what happens when the database is down?",
		}
	case RoleEngineer2:
		return []string{
			"why don't we just try it?",
			"nah, too complicated — simpler way exists",
			"oh wait, what if we also did X",
			"haha tbh the first version was better",
			"ok i was wrong, the other way works better",
		}
	case RoleEngineer3:
		return []string{
			"we're solving this same problem in three places",
			"that's going to scale poorly",
			"if we just add a Makefile target for this, nobody has to remember the flags",
			"interesting",
			"the CI is going to choke on this",
		}
	case RoleReviewer:
		return []string{
			"blocking: this swallows the error on line 42",
			"nit: naming — fetchUser reads better than getU",
			"approved — clean work",
			"changes requested — the auth check is missing on the delete endpoint",
			"nice catch on the race condition, that would have bitten us",
		}
	case RoleDesigner:
		return []string{
			"this feels off — the user has to click three times to do one thing",
			"imagine you're a new user seeing this for the first time",
			"the spacing is doing a lot of heavy lifting here, in a good way",
			"why can't this just be one screen?",
			"I like what Mara did with the loading state — that's thoughtful",
		}
	}
	return nil
}

func rosterTitle(role Role) string {
	switch role {
	case RolePM:
		return "PM"
	case RoleTechLead:
		return "Tech Lead"
	case RoleEngineer1, RoleEngineer2, RoleEngineer3:
		return "Engineer"
	case RoleReviewer:
		return "Reviewer"
	case RoleDesigner:
		return "Designer"
	}
	return string(role)
}
