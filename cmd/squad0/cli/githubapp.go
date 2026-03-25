package cli

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
	gh "github.com/JR-G/squad0/internal/integrations/github"
	"github.com/JR-G/squad0/internal/tui"
)

// OptionalSecretGetter can retrieve optional secrets that may not exist.
type OptionalSecretGetter interface {
	GetOptional(ctx context.Context, name string) (string, error)
}

func configureGitHubAppToken(ctx context.Context, agents map[agent.Role]*agent.Agent, loader SecretLoader, out io.Writer) {
	appID, installID, privateKey := loadGitHubAppSecrets(ctx, loader)
	if appID == "" || installID == "" || privateKey == "" {
		_, _ = fmt.Fprint(out, tui.StepWarn("GitHub App not configured — reviews use owner token"))
		return
	}

	applyGitHubAppTokenWithURL(ctx, agents, appID, installID, privateKey, "", out)
}

func applyGitHubAppTokenWithURL(ctx context.Context, agents map[agent.Role]*agent.Agent, appID, installID, privateKey, apiURL string, out io.Writer) {
	provider, err := createAppTokenProvider(appID, installID, privateKey, apiURL)
	if err != nil {
		_, _ = fmt.Fprint(out, tui.StepFail(fmt.Sprintf("GitHub App key invalid: %v", err)))
		return
	}

	token, tokenErr := provider.Token(ctx)
	if tokenErr != nil {
		_, _ = fmt.Fprint(out, tui.StepFail(fmt.Sprintf("GitHub App token failed: %v", tokenErr)))
		return
	}

	for _, role := range []agent.Role{agent.RoleReviewer, agent.RolePM, agent.RoleTechLead} {
		if a, ok := agents[role]; ok {
			a.SetGHToken(token)
		}
	}

	_, _ = fmt.Fprint(out, tui.StepDone("GitHub App token configured for reviews"))
}

func loadGitHubAppSecrets(ctx context.Context, loader SecretLoader) (appID, installID, privateKey string) {
	getter, ok := loader.(OptionalSecretGetter)
	if !ok {
		return "", "", ""
	}

	appID, _ = getter.GetOptional(ctx, "GITHUB_APP_ID")
	installID, _ = getter.GetOptional(ctx, "GITHUB_APP_INSTALLATION_ID")

	encoded, _ := getter.GetOptional(ctx, "GITHUB_APP_PRIVATE_KEY")
	privateKey = decodePrivateKey(encoded)

	return appID, installID, privateKey
}

// decodePrivateKey handles both raw PEM and base64-encoded PEM.
// Keychain strips newlines from multi-line values, so the PEM is
// stored as base64 and decoded here.
func decodePrivateKey(encoded string) string {
	if strings.HasPrefix(encoded, "-----BEGIN") {
		return encoded
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return encoded
	}
	return string(decoded)
}

func createAppTokenProvider(appID, installID, privateKey, apiURL string) (*gh.AppTokenProvider, error) {
	if apiURL != "" {
		return gh.NewAppTokenProviderWithURL(appID, installID, privateKey, apiURL)
	}
	return gh.NewAppTokenProvider(appID, installID, privateKey)
}
