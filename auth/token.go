package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"epic_lab_reporter/config"
)

// tokenResponse is the JSON body Epic returns on a successful token exchange.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"` // seconds
	TokenType   string `json:"token_type"`
}

// TokenClient fetches and caches a SMART Backend Services access token.
type TokenClient struct {
	cfg        *config.Config
	mu         sync.Mutex
	token      string
	expiresAt  time.Time
}

// NewTokenClient creates a TokenClient. Call GetToken() to fetch the first token.
func NewTokenClient(cfg *config.Config) *TokenClient {
	return &TokenClient{cfg: cfg}
}

// GetToken returns a valid access token, refreshing if it expires within 60 seconds.
func (tc *TokenClient) GetToken() (string, error) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	if tc.token != "" && time.Now().Before(tc.expiresAt.Add(-60*time.Second)) {
		return tc.token, nil
	}

	token, expiresIn, err := tc.fetchToken()
	if err != nil {
		return "", err
	}

	tc.token = token
	tc.expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
	return tc.token, nil
}

// fetchToken builds a fresh JWT and POSTs it to Epic's token endpoint.
func (tc *TokenClient) fetchToken() (string, int, error) {
	privKey, err := LoadPrivateKey(tc.cfg.EpicPrivateKeyPath)
	if err != nil {
		return "", 0, fmt.Errorf("load private key: %w", err)
	}

	pubKey, err := LoadPublicKey(tc.cfg.EpicPublicKeyPath)
	if err != nil {
		return "", 0, fmt.Errorf("load public key: %w", err)
	}

	kid, err := DeriveKID(pubKey)
	if err != nil {
		return "", 0, fmt.Errorf("derive kid: %w", err)
	}

	assertion, err := BuildAssertion(tc.cfg, privKey, kid)
	if err != nil {
		return "", 0, fmt.Errorf("build assertion: %w", err)
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_assertion_type", "urn:ietf:params:oauth:client-assertion-type:jwt-bearer")
	form.Set("client_assertion", assertion)
	// Request only the scopes the app is registered for in the Epic portal.
	// system/Group.read is required for the Group/$export bulk data endpoint.
	form.Set("scope", "system/Patient.read system/DiagnosticReport.read system/Group.read system/Observation.read")

	resp, err := http.PostForm(tc.cfg.EpicTokenURL, form)
	if err != nil {
		return "", 0, fmt.Errorf("POST token endpoint: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", 0, fmt.Errorf("decode token response: %w", err)
	}

	if tr.AccessToken == "" {
		return "", 0, fmt.Errorf("token response missing access_token field")
	}

	return tr.AccessToken, tr.ExpiresIn, nil
}
