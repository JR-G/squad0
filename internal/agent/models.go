package agent

// Role represents an agent's function within the team.
type Role string

const (
	// RolePM is the project manager who assigns work and runs rituals.
	RolePM Role = "pm"
	// RoleTechLead is the technical lead who reviews architecture.
	RoleTechLead Role = "tech-lead"
	// RoleEngineer1 is the first engineer, thorough and backend-leaning.
	RoleEngineer1 Role = "engineer-1"
	// RoleEngineer2 is the second engineer, fast and frontend-leaning.
	RoleEngineer2 Role = "engineer-2"
	// RoleEngineer3 is the third engineer, architectural and infra-leaning.
	RoleEngineer3 Role = "engineer-3"
	// RoleReviewer is the code reviewer and quality gate.
	RoleReviewer Role = "reviewer"
	// RoleDesigner is the UI/UX critic.
	RoleDesigner Role = "designer"
)

// AllRoles returns all defined agent roles.
func AllRoles() []Role {
	return []Role{
		RolePM,
		RoleTechLead,
		RoleEngineer1,
		RoleEngineer2,
		RoleEngineer3,
		RoleReviewer,
		RoleDesigner,
	}
}

// PersonalityFile returns the filename for the role's personality template.
func (role Role) PersonalityFile() string {
	return string(role) + ".md"
}

// ModelForRole returns the Claude model identifier for the given role
// using the provided model configuration map.
func ModelForRole(role Role, models map[Role]string) string {
	model, ok := models[role]
	if !ok {
		return ""
	}
	return model
}
