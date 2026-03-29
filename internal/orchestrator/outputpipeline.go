package orchestrator

import "github.com/JR-G/squad0/internal/agent"

// OutputPipeline validates and filters agent responses before posting
// to Slack. Enforces voice rules structurally — models cannot bypass.
type OutputPipeline struct {
	rules map[agent.Role]VoiceRules
}

// NewOutputPipeline creates a pipeline with default rules for all roles.
func NewOutputPipeline() *OutputPipeline {
	rules := make(map[agent.Role]VoiceRules, len(agent.AllRoles()))
	for _, role := range agent.AllRoles() {
		rules[role] = DefaultVoiceRules(role)
	}

	return &OutputPipeline{rules: rules}
}

// RulesForRole returns the voice rules for the given role.
func (pipeline *OutputPipeline) RulesForRole(role agent.Role) VoiceRules {
	rules, ok := pipeline.rules[role]
	if !ok {
		return DefaultVoiceRules(role)
	}

	return rules
}

// Process validates a response against the role's voice rules and
// recent messages. Returns the text unchanged if valid, or empty
// string with the failure reason.
func (pipeline *OutputPipeline) Process(text string, role agent.Role, recentMessages []string) (string, ValidationResult) {
	rules := pipeline.RulesForRole(role)
	result := ValidateResponse(text, rules, recentMessages)

	if !result.OK {
		return "", result
	}

	return text, result
}
