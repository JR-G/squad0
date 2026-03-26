package orchestrator

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
)

// RunWitnessScan has the Tech Lead and PM scan active channels for
// unanswered questions and stale discussions, then weigh in. This
// is the Gas Town "Witness pattern" — proactive scanning, not just
// reactive responses.
func (orch *Orchestrator) RunWitnessScan(ctx context.Context) {
	if orch.conversation == nil {
		return
	}

	for _, channel := range []string{"engineering", "reviews"} {
		orch.scanChannel(ctx, channel)
	}
}

func (orch *Orchestrator) scanChannel(ctx context.Context, channel string) {
	lines := orch.conversation.RecentMessages(channel)
	if len(lines) < 2 {
		return
	}

	lastLine := lines[len(lines)-1]

	// If the last message is a question, have PM or Tech Lead answer.
	if !containsQuestion(lastLine) {
		return
	}

	// Don't respond if the question is from PM or Tech Lead (they asked it).
	if strings.Contains(lastLine, string(agent.RolePM)) || strings.Contains(lastLine, string(agent.RoleTechLead)) {
		return
	}

	// Check roster names too.
	pmName := orch.NameForRole(agent.RolePM)
	tlName := orch.NameForRole(agent.RoleTechLead)
	if strings.Contains(lastLine, pmName) || strings.Contains(lastLine, tlName) {
		return
	}

	// Pick PM for process questions, Tech Lead for technical questions.
	role := orch.pickWitnessRole(lastLine)
	witnessAgent, ok := orch.agents[role]
	if !ok {
		return
	}

	if orch.isRoleBusy(ctx, role) {
		return
	}

	tail := lines
	if len(tail) > 5 {
		tail = tail[len(tail)-5:]
	}

	prompt := fmt.Sprintf(
		"You're scanning #%s and noticed an unanswered question. "+
			"Reply naturally — don't say you're scanning, just respond to the question:\n\n%s",
		channel, strings.Join(tail, "\n"))

	response, err := witnessAgent.QuickChat(ctx, prompt)
	if err != nil {
		return
	}

	response = filterPassResponse(response)
	if response == "" {
		return
	}

	log.Printf("witness: %s responding to unanswered question in #%s", role, channel)
	orch.postAsRole(ctx, channel, response, role)
}

func (orch *Orchestrator) pickWitnessRole(question string) agent.Role {
	techSignals := []string{
		"architecture", "design", "pattern", "module", "boundary",
		"interface", "dependency", "scale", "refactor", "split",
	}

	lower := strings.ToLower(question)
	for _, signal := range techSignals {
		if strings.Contains(lower, signal) {
			return agent.RoleTechLead
		}
	}

	return agent.RolePM
}

func (orch *Orchestrator) isRoleBusy(ctx context.Context, role agent.Role) bool {
	checkIn, err := orch.checkIns.GetByAgent(ctx, role)
	if err != nil {
		return false
	}
	return checkIn.Status != "idle"
}
