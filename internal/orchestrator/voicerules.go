package orchestrator

import "github.com/JR-G/squad0/internal/agent"

// VoiceRules defines measurable output constraints for an agent's chat
// responses. Enforced structurally — models cannot ignore these.
type VoiceRules struct {
	MaxChars        int
	MaxSentences    int
	RejectIfSimilar float64 // Jaccard bigram threshold (0.0–1.0).
}

// DefaultVoiceRules returns per-role output constraints derived from
// each agent's personality. These enforce voice structurally — the
// model's prompt says "laconic" but this guarantees it.
func DefaultVoiceRules(role agent.Role) VoiceRules {
	base := VoiceRules{
		MaxChars:        300,
		MaxSentences:    3,
		RejectIfSimilar: 0.75,
	}

	switch role {
	case agent.RolePM:
		base.MaxChars = 400
		base.MaxSentences = 5
	case agent.RoleTechLead:
		base.MaxChars = 400
		base.MaxSentences = 4
	case agent.RoleEngineer1:
		base.MaxChars = 350
		base.MaxSentences = 3
	case agent.RoleEngineer2:
		base.MaxChars = 300
		base.MaxSentences = 3
	case agent.RoleEngineer3:
		base.MaxChars = 350
		base.MaxSentences = 3
	case agent.RoleReviewer:
		base.MaxChars = 350
		base.MaxSentences = 3
	case agent.RoleDesigner:
		base.MaxChars = 350
		base.MaxSentences = 3
	}

	return base
}
