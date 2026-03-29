package orchestrator

import "github.com/JR-G/squad0/internal/agent"

// VoiceRules defines measurable output constraints for an agent's chat
// responses. Enforced structurally — models cannot ignore these.
type VoiceRules struct {
	MaxChars        int
	MaxSentences    int
	BannedPhrases   []string
	RejectIfSimilar float64 // Jaccard bigram threshold (0.0–1.0).
}

// sharedBannedPhrases are filtered from ALL agent responses.
var sharedBannedPhrases = []string{
	// Capability listing.
	"i can help with",
	"my capabilities",
	"i'm able to",
	"i'm ready to work with you",
	"i understand. i'm ready",
	"i'll adopt this approach",
	"key things i'm holding",
	"i understand the project",
	"ready to ship",
	"what are you working on",
	// Asking what to do.
	"what would you like",
	"what should i",
	"what do you need",
	"what's the task",
	"what do you want me to",
	// Claiming actions from chat.
	"i'll go ahead",
	"i'm merging",
	"i'll fix that",
	"i'll deploy",
	"on it!",
	// AI identity leak.
	"i've read the claude",
	"as an ai",
	"i don't have the ability",
}

// DefaultVoiceRules returns per-role output constraints derived from
// each agent's personality. These enforce voice structurally — the
// model's prompt says "laconic" but this guarantees it.
func DefaultVoiceRules(role agent.Role) VoiceRules {
	base := VoiceRules{
		MaxChars:        300,
		MaxSentences:    3,
		BannedPhrases:   sharedBannedPhrases,
		RejectIfSimilar: 0.75,
	}

	switch role {
	case agent.RolePM:
		base.MaxChars = 280
		base.MaxSentences = 3
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
