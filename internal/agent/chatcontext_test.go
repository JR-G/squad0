package agent_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewChatContext_CreatesDirectory(t *testing.T) {
	t.Parallel()

	roster := map[agent.Role]string{
		agent.RoleEngineer1: "Callum",
		agent.RoleEngineer2: "Mara",
		agent.RoleTechLead:  "Sable",
	}

	ctx, err := agent.NewChatContext(agent.RoleEngineer2, roster, nil, "")
	require.NoError(t, err)
	defer ctx.Cleanup()

	assert.DirExists(t, ctx.Dir())
	assert.FileExists(t, filepath.Join(ctx.Dir(), "CLAUDE.md"))
}

func TestNewChatContext_CLAUDEMDContainsIdentity(t *testing.T) {
	t.Parallel()

	roster := map[agent.Role]string{
		agent.RoleEngineer2: "Mara",
		agent.RoleTechLead:  "Sable",
	}

	ctx, err := agent.NewChatContext(agent.RoleEngineer2, roster, nil, "")
	require.NoError(t, err)
	defer ctx.Cleanup()

	content, err := os.ReadFile(filepath.Join(ctx.Dir(), "CLAUDE.md"))
	require.NoError(t, err)

	md := string(content)
	assert.Contains(t, md, "# Persona: Mara")
	assert.Contains(t, md, "You are playing Mara")
	assert.Contains(t, md, "## Examples of how you talk")
	assert.Contains(t, md, "why don't we just try it?")
	assert.Contains(t, md, "Sable — tech lead")
}

func TestNewChatContext_IncludesBeliefs(t *testing.T) {
	t.Parallel()

	roster := map[agent.Role]string{agent.RoleEngineer1: "Callum"}
	beliefs := []string{"the auth module has fragile error handling", "always add timeout to Stripe calls"}

	ctx, err := agent.NewChatContext(agent.RoleEngineer1, roster, beliefs, "")
	require.NoError(t, err)
	defer ctx.Cleanup()

	content, err := os.ReadFile(filepath.Join(ctx.Dir(), "CLAUDE.md"))
	require.NoError(t, err)

	md := string(content)
	assert.Contains(t, md, "## Things you know from experience")
	assert.Contains(t, md, "auth module has fragile error handling")
	assert.Contains(t, md, "always add timeout to Stripe calls")
}

func TestNewChatContext_NoBeliefs_OmitsSection(t *testing.T) {
	t.Parallel()

	roster := map[agent.Role]string{agent.RoleEngineer3: "Cormac"}

	ctx, err := agent.NewChatContext(agent.RoleEngineer3, roster, nil, "")
	require.NoError(t, err)
	defer ctx.Cleanup()

	content, err := os.ReadFile(filepath.Join(ctx.Dir(), "CLAUDE.md"))
	require.NoError(t, err)

	assert.NotContains(t, string(content), "Things you know from experience")
}

func TestNewChatContext_AllRoles_HaveExamples(t *testing.T) {
	t.Parallel()

	roles := agent.AllRoles()
	roster := make(map[agent.Role]string, len(roles))
	for _, role := range roles {
		roster[role] = string(role)
	}

	for _, role := range roles {
		ctx, err := agent.NewChatContext(role, roster, nil, "")
		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(ctx.Dir(), "CLAUDE.md"))
		require.NoError(t, err)
		assert.Contains(t, string(content), "## Examples of how you talk", "role %s should have voice examples", role)

		ctx.Cleanup()
	}
}

func TestChatContext_Cleanup_RemovesDir(t *testing.T) {
	t.Parallel()

	ctx, err := agent.NewChatContext(agent.RolePM, map[agent.Role]string{agent.RolePM: "Morgan"}, nil, "")
	require.NoError(t, err)

	dir := ctx.Dir()
	assert.DirExists(t, dir)

	ctx.Cleanup()
	assert.NoDirExists(t, dir)
}

func TestNewChatContext_FallbackName(t *testing.T) {
	t.Parallel()

	// No roster entry — should fall back to role ID.
	ctx, err := agent.NewChatContext(agent.RolePM, nil, nil, "")
	require.NoError(t, err)
	defer ctx.Cleanup()

	content, err := os.ReadFile(filepath.Join(ctx.Dir(), "CLAUDE.md"))
	require.NoError(t, err)

	assert.Contains(t, string(content), "# Persona: pm")
}

func TestSetChatContext_StoresRosterAndBeliefs(t *testing.T) {
	t.Parallel()

	// SetChatContext and SetDefaultWorkDir are simple setters.
	// Verify they don't panic.
	a := agent.NewAgent(agent.RoleEngineer1, "test", nil, nil, nil, nil, nil, nil)
	a.SetChatContext(map[agent.Role]string{agent.RoleEngineer1: "Callum"}, []string{"test belief"}, "dry and understated")
	a.SetDefaultWorkDir("/tmp/test")
}

func TestNewChatContext_WithVoiceText_IncludesVoiceSection(t *testing.T) {
	t.Parallel()

	roster := map[agent.Role]string{agent.RoleEngineer1: "Callum"}
	voice := "Dry, understated, slightly wary. You say things like 'I'm not convinced this handles the timeout case'."

	ctx, err := agent.NewChatContext(agent.RoleEngineer1, roster, nil, voice)
	require.NoError(t, err)
	defer ctx.Cleanup()

	content, err := os.ReadFile(filepath.Join(ctx.Dir(), "CLAUDE.md"))
	require.NoError(t, err)

	md := string(content)
	assert.Contains(t, md, "## Your voice")
	assert.Contains(t, md, "Dry, understated, slightly wary")
}

func TestNewChatContext_WithoutVoiceText_OmitsVoiceSection(t *testing.T) {
	t.Parallel()

	roster := map[agent.Role]string{agent.RoleEngineer1: "Callum"}

	ctx, err := agent.NewChatContext(agent.RoleEngineer1, roster, nil, "")
	require.NoError(t, err)
	defer ctx.Cleanup()

	content, err := os.ReadFile(filepath.Join(ctx.Dir(), "CLAUDE.md"))
	require.NoError(t, err)

	assert.NotContains(t, string(content), "## Your voice")
}

func TestBuildPersonalityCLAUDEMDForPrime_ReturnsPersonality(t *testing.T) {
	t.Parallel()

	roster := map[agent.Role]string{agent.RoleEngineer1: "Callum"}
	output := agent.BuildPersonalityCLAUDEMDForPrime(agent.RoleEngineer1, roster, nil, "dry and understated")

	assert.Contains(t, output, "# Persona: Callum")
	assert.Contains(t, output, "dry and understated")
}

func TestBuildPersonalityCLAUDEMDForPrime_FallbackName(t *testing.T) {
	t.Parallel()

	roster := map[agent.Role]string{}
	output := agent.BuildPersonalityCLAUDEMDForPrime(agent.RolePM, roster, nil, "")

	assert.Contains(t, output, "# Persona: pm")
}

func TestNewChatContext_AllRoles_HaveAntiPatterns(t *testing.T) {
	t.Parallel()

	roles := agent.AllRoles()
	roster := make(map[agent.Role]string, len(roles))
	for _, role := range roles {
		roster[role] = string(role)
	}

	for _, role := range roles {
		ctx, err := agent.NewChatContext(role, roster, nil, "")
		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(ctx.Dir(), "CLAUDE.md"))
		require.NoError(t, err)
		assert.Contains(t, string(content), "## You would NEVER say", "role %s should have anti-patterns", role)

		ctx.Cleanup()
	}
}
