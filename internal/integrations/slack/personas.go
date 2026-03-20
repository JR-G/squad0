package slack

import "github.com/JR-G/squad0/internal/agent"

// Persona holds the display identity for an agent in Slack.
type Persona struct {
	Username string
	IconURL  string
}

// DefaultPersonas returns the standard persona mapping for all agent roles.
func DefaultPersonas() map[agent.Role]Persona {
	return map[agent.Role]Persona{
		agent.RolePM: {
			Username: "PM",
			IconURL:  "",
		},
		agent.RoleTechLead: {
			Username: "Tech Lead",
			IconURL:  "",
		},
		agent.RoleEngineer1: {
			Username: "Engineer 1",
			IconURL:  "",
		},
		agent.RoleEngineer2: {
			Username: "Engineer 2",
			IconURL:  "",
		},
		agent.RoleEngineer3: {
			Username: "Engineer 3",
			IconURL:  "",
		},
		agent.RoleReviewer: {
			Username: "Reviewer",
			IconURL:  "",
		},
		agent.RoleDesigner: {
			Username: "Designer",
			IconURL:  "",
		},
	}
}

// PersonaForRole returns the persona for the given role, falling back to
// the role name if no persona is configured.
func PersonaForRole(role agent.Role, personas map[agent.Role]Persona) Persona {
	persona, ok := personas[role]
	if !ok {
		return Persona{Username: string(role)}
	}
	return persona
}
