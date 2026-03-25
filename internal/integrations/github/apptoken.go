package github

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AppTokenProvider generates GitHub App installation tokens. The token
// is cached and refreshed automatically when it expires.
type AppTokenProvider struct {
	appID          string
	installationID string
	privateKey     *rsa.PrivateKey
	httpClient     *http.Client
	apiURL         string

	mu           sync.Mutex
	cachedToken  string
	cachedExpiry time.Time
}

// NewAppTokenProvider creates a provider from the app credentials.
// The privateKeyPEM is the contents of the .pem file downloaded from
// GitHub when creating the app.
func NewAppTokenProvider(appID, installationID, privateKeyPEM string) (*AppTokenProvider, error) {
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(privateKeyPEM))
	if err != nil {
		return nil, fmt.Errorf("parsing GitHub App private key: %w", err)
	}

	return &AppTokenProvider{
		appID:          appID,
		installationID: installationID,
		privateKey:     key,
		httpClient:     &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// NewAppTokenProviderWithURL creates a provider that targets a custom
// API URL. Used in testing to point at a mock server.
func NewAppTokenProviderWithURL(appID, installationID, privateKeyPEM, apiURL string) (*AppTokenProvider, error) {
	provider, err := NewAppTokenProvider(appID, installationID, privateKeyPEM)
	if err != nil {
		return nil, err
	}
	provider.apiURL = apiURL
	return provider, nil
}

// Token returns a valid installation token, refreshing if needed.
// The token is good for gh CLI commands — set GH_TOKEN env var.
func (provider *AppTokenProvider) Token(ctx context.Context) (string, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()

	// Return cached token if still valid (with 5 min buffer).
	if provider.cachedToken != "" && time.Now().Before(provider.cachedExpiry.Add(-5*time.Minute)) {
		return provider.cachedToken, nil
	}

	appJWT, err := provider.generateJWT()
	if err != nil {
		return "", fmt.Errorf("generating app JWT: %w", err)
	}

	token, expiry, err := provider.exchangeForInstallationToken(ctx, appJWT)
	if err != nil {
		return "", fmt.Errorf("exchanging for installation token: %w", err)
	}

	provider.cachedToken = token
	provider.cachedExpiry = expiry

	return token, nil
}

func (provider *AppTokenProvider) generateJWT() (string, error) {
	now := time.Now()

	claims := jwt.RegisteredClaims{
		Issuer:    provider.appID,
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)),
		ExpiresAt: jwt.NewNumericDate(now.Add(10 * time.Minute)),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(provider.privateKey)
}

type installationTokenResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (provider *AppTokenProvider) exchangeForInstallationToken(ctx context.Context, appJWT string) (string, time.Time, error) {
	baseURL := "https://api.github.com"
	if provider.apiURL != "" {
		baseURL = provider.apiURL
	}
	url := fmt.Sprintf("%s/app/installations/%s/access_tokens", baseURL, provider.installationID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, http.NoBody)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := provider.httpClient.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("requesting installation token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		return "", time.Time{}, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, body)
	}

	var tokenResp installationTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", time.Time{}, fmt.Errorf("parsing token response: %w", err)
	}

	return tokenResp.Token, tokenResp.ExpiresAt, nil
}
