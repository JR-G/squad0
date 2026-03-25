package github_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gh "github.com/JR-G/squad0/internal/integrations/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func generateTestPEM(t *testing.T) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	keyBytes := x509.MarshalPKCS1PrivateKey(key)
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyBytes}

	return string(pem.EncodeToMemory(block))
}

func TestNewAppTokenProvider_ValidKey(t *testing.T) {
	t.Parallel()

	pemData := generateTestPEM(t)
	provider, err := gh.NewAppTokenProvider("123", "456", pemData)

	require.NoError(t, err)
	assert.NotNil(t, provider)
}

func TestNewAppTokenProvider_InvalidKey(t *testing.T) {
	t.Parallel()

	_, err := gh.NewAppTokenProvider("123", "456", "not a pem")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing GitHub App private key")
}

func TestToken_ExchangesJWTForInstallationToken(t *testing.T) {
	t.Parallel()

	pemData := generateTestPEM(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.Header.Get("Authorization"), "Bearer ")
		assert.Equal(t, http.MethodPost, r.Method)

		resp := map[string]interface{}{
			"token":      "ghs_test_installation_token",
			"expires_at": time.Now().Add(time.Hour).Format(time.RFC3339),
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(server.Close)

	provider, err := gh.NewAppTokenProviderWithURL("123", "456", pemData, server.URL)
	require.NoError(t, err)

	token, tokenErr := provider.Token(context.Background())
	require.NoError(t, tokenErr)
	assert.Equal(t, "ghs_test_installation_token", token)
}

func TestToken_CachesToken(t *testing.T) {
	t.Parallel()

	pemData := generateTestPEM(t)

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		resp := map[string]interface{}{
			"token":      "ghs_cached",
			"expires_at": time.Now().Add(time.Hour).Format(time.RFC3339),
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(server.Close)

	provider, err := gh.NewAppTokenProviderWithURL("123", "456", pemData, server.URL)
	require.NoError(t, err)

	ctx := context.Background()
	token1, _ := provider.Token(ctx)
	token2, _ := provider.Token(ctx)

	assert.Equal(t, token1, token2)
	assert.Equal(t, 1, callCount, "should only call GitHub API once")
}

func TestToken_APIError_ReturnsError(t *testing.T) {
	t.Parallel()

	pemData := generateTestPEM(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	t.Cleanup(server.Close)

	provider, err := gh.NewAppTokenProviderWithURL("123", "456", pemData, server.URL)
	require.NoError(t, err)

	_, tokenErr := provider.Token(context.Background())
	require.Error(t, tokenErr)
	assert.Contains(t, tokenErr.Error(), "401")
}

func TestToken_MalformedJSON_ReturnsError(t *testing.T) {
	t.Parallel()

	pemData := generateTestPEM(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`not json`))
	}))
	t.Cleanup(server.Close)

	provider, err := gh.NewAppTokenProviderWithURL("123", "456", pemData, server.URL)
	require.NoError(t, err)

	_, tokenErr := provider.Token(context.Background())
	require.Error(t, tokenErr)
	assert.Contains(t, tokenErr.Error(), "parsing token response")
}
