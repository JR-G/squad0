package secrets

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

const (
	// ServiceName is the macOS Keychain service name under which all
	// squad0 secrets are stored.
	ServiceName = "squad0"
)

// RequiredSecrets lists the secret names that must be configured for
// squad0 to operate.
var RequiredSecrets = []string{
	"SLACK_BOT_TOKEN",
	"SLACK_APP_TOKEN",
}

// Secrets holds all resolved secret values in memory for the process
// lifetime. Values are never logged or serialised.
type Secrets struct {
	SlackBotToken string
	SlackAppToken string
}

// Manager handles secret lifecycle operations including retrieval,
// storage, and verification against the required secrets list.
type Manager struct {
	keychain *Keychain
}

// NewManager creates a Manager backed by the given Keychain.
func NewManager(keychain *Keychain) *Manager {
	return &Manager{keychain: keychain}
}

// LoadAll retrieves all required secrets from the Keychain and returns
// them as a Secrets struct. Returns an error listing all missing secrets
// if any are absent.
func (mgr *Manager) LoadAll(ctx context.Context) (Secrets, error) {
	values := make(map[string]string, len(RequiredSecrets))
	var missing []string

	for _, name := range RequiredSecrets {
		value, err := mgr.keychain.Get(ctx, name)
		if errors.Is(err, ErrSecretNotFound) {
			missing = append(missing, name)
			continue
		}
		if err != nil {
			return Secrets{}, fmt.Errorf("loading secret %s: %w", name, err)
		}
		values[name] = value
	}

	if len(missing) > 0 {
		return Secrets{}, fmt.Errorf("missing required secrets: %s", strings.Join(missing, ", "))
	}

	return Secrets{
		SlackBotToken: values["SLACK_BOT_TOKEN"],
		SlackAppToken: values["SLACK_APP_TOKEN"],
	}, nil
}

// Verify checks whether all required secrets exist in the Keychain
// without retrieving their values. Returns a map of secret name to
// presence status, and an error if any are missing.
func (mgr *Manager) Verify(ctx context.Context) (map[string]bool, error) {
	status, err := mgr.checkPresence(ctx)
	if err != nil {
		return nil, err
	}

	var missing []string
	for _, name := range RequiredSecrets {
		if !status[name] {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		return status, fmt.Errorf("missing required secrets: %s", strings.Join(missing, ", "))
	}

	return status, nil
}

// List returns the names of all required secrets and whether each is
// currently stored in the Keychain. Never returns secret values.
func (mgr *Manager) List(ctx context.Context) (map[string]bool, error) {
	return mgr.checkPresence(ctx)
}

// Set stores a secret value in the Keychain. The name must be one of the
// recognised required secrets.
func (mgr *Manager) Set(ctx context.Context, name, value string) error {
	if !slices.Contains(RequiredSecrets, name) {
		return fmt.Errorf("unrecognised secret name %q; valid names: %s",
			name, strings.Join(RequiredSecrets, ", "))
	}

	return mgr.keychain.Set(ctx, name, value)
}

func (mgr *Manager) checkPresence(ctx context.Context) (map[string]bool, error) {
	status := make(map[string]bool, len(RequiredSecrets))

	for _, name := range RequiredSecrets {
		exists, err := mgr.keychain.Exists(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("checking secret %s: %w", name, err)
		}
		status[name] = exists
	}

	return status, nil
}
