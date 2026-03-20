package secrets

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrSecretNotFound is returned when a requested secret does not exist
// in the Keychain.
var ErrSecretNotFound = errors.New("secret not found in keychain")

// CommandRunner executes shell commands and returns their combined output.
// This interface exists to enable testing without touching the real Keychain.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner implements CommandRunner using os/exec.
type ExecRunner struct{}

// Run executes the named command with the given arguments.
func (runner ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// Keychain reads and writes secrets in the macOS Keychain under a fixed
// service name.
type Keychain struct {
	serviceName string
	runner      CommandRunner
}

// NewKeychain creates a Keychain that stores secrets under the given
// service name.
func NewKeychain(serviceName string, runner CommandRunner) *Keychain {
	return &Keychain{
		serviceName: serviceName,
		runner:      runner,
	}
}

// Get retrieves the secret value for the given key from the Keychain.
// Returns ErrSecretNotFound if the key does not exist.
func (kc *Keychain) Get(ctx context.Context, key string) (string, error) {
	output, err := kc.runner.Run(ctx,
		"security", "find-generic-password",
		"-s", kc.serviceName,
		"-a", key,
		"-w",
	)
	if err == nil {
		return strings.TrimSpace(string(output)), nil
	}

	if isItemNotFound(err, output) {
		return "", ErrSecretNotFound
	}

	return "", fmt.Errorf("keychain get %s: %w", key, err)
}

// Set stores a secret value for the given key in the Keychain. If the key
// already exists, it is updated.
func (kc *Keychain) Set(ctx context.Context, key, value string) error {
	_, err := kc.runner.Run(ctx,
		"security", "add-generic-password",
		"-s", kc.serviceName,
		"-a", key,
		"-w", value,
		"-U",
	)
	if err != nil {
		return fmt.Errorf("keychain set %s: %w", key, err)
	}

	return nil
}

// Exists checks whether a secret with the given key is stored in the
// Keychain without retrieving its value.
func (kc *Keychain) Exists(ctx context.Context, key string) (bool, error) {
	output, err := kc.runner.Run(ctx,
		"security", "find-generic-password",
		"-s", kc.serviceName,
		"-a", key,
	)
	if err == nil {
		return true, nil
	}

	if isItemNotFound(err, output) {
		return false, nil
	}

	return false, fmt.Errorf("keychain exists %s: %w", key, err)
}

// Delete removes the secret with the given key from the Keychain.
// Returns ErrSecretNotFound if the key does not exist.
func (kc *Keychain) Delete(ctx context.Context, key string) error {
	output, err := kc.runner.Run(ctx,
		"security", "delete-generic-password",
		"-s", kc.serviceName,
		"-a", key,
	)
	if err == nil {
		return nil
	}

	if isItemNotFound(err, output) {
		return ErrSecretNotFound
	}

	return fmt.Errorf("keychain delete %s: %w", key, err)
}

func isItemNotFound(err error, output []byte) bool {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 44 {
		return true
	}

	return strings.Contains(string(output), "could not be found")
}
