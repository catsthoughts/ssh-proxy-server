package sso

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	DefaultProvider              = "keycloak"
	DefaultBaseURL               = "http://localhost:8080"
	DefaultRealm                 = "ssh-proxy-server"
	DefaultClientID              = "ssh-proxy-server"
	DefaultScope                 = "openid"
	DefaultAuthTimeoutSeconds    = 120
	DefaultPollIntervalSeconds   = 5
	DefaultRequestTimeoutSeconds = 10
)

type Config struct {
	Enabled            bool
	Provider           string
	BaseURL            string
	Realm              string
	ClientID           string
	ClientSecret       string
	Scope              string
	AuthTimeout        time.Duration
	PollInterval       time.Duration
	RequestTimeout     time.Duration
	EnforceUserMatch   bool
	InsecureSkipVerify bool
}

// Identity represents the user identity returned by the SSO provider.
type Identity struct {
	Subject           string
	PreferredUsername string
	Email             string
}

func (i Identity) BestIdentifier() string {
	for _, value := range []string{i.PreferredUsername, i.Email, i.Subject} {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (i Identity) MatchesSSHUser(user string) bool {
	user = strings.ToLower(strings.TrimSpace(user))
	if user == "" {
		return false
	}

	candidates := []string{i.PreferredUsername, i.Email, i.Subject}
	for _, candidate := range candidates {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if candidate == "" {
			continue
		}
		if candidate == user {
			return true
		}
		if at := strings.Index(candidate, "@"); at > 0 && candidate[:at] == user {
			return true
		}
	}
	return false
}

type discoveryDocument struct {
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
}

type deviceAuthorizationResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type tokenErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type tokenSuccessResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	TokenType   string `json:"token_type"`
}

func NormalizeProvider(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return DefaultProvider
	}
	return provider
}

func IsSupportedProvider(provider string) bool {
	return NormalizeProvider(provider) == DefaultProvider
}

func issuerURL(cfg Config) string {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	realm := strings.Trim(strings.TrimSpace(cfg.Realm), "/")
	return baseURL + "/realms/" + url.PathEscape(realm)
}

func AuthenticateDeviceFlow(ctx context.Context, cfg Config, output io.Writer) (Identity, error) {
	cfg.Provider = NormalizeProvider(cfg.Provider)
	if !IsSupportedProvider(cfg.Provider) {
		return Identity{}, fmt.Errorf("unsupported SSO provider %q", cfg.Provider)
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	if strings.TrimSpace(cfg.Realm) == "" {
		cfg.Realm = DefaultRealm
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		cfg.ClientID = DefaultClientID
	}
	if strings.TrimSpace(cfg.Scope) == "" {
		cfg.Scope = DefaultScope
	}
	if cfg.AuthTimeout <= 0 {
		cfg.AuthTimeout = time.Duration(DefaultAuthTimeoutSeconds) * time.Second
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = time.Duration(DefaultPollIntervalSeconds) * time.Second
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = time.Duration(DefaultRequestTimeoutSeconds) * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, cfg.AuthTimeout)
	defer cancel()

	discovery, err := fetchDiscoveryDocument(ctx, cfg)
	if err != nil {
		return Identity{}, err
	}
	deviceAuth, err := startDeviceAuthorization(ctx, cfg, discovery.DeviceAuthorizationEndpoint)
	if err != nil {
		return Identity{}, err
	}

	if output != nil {
		link := strings.TrimSpace(deviceAuth.VerificationURIComplete)
		if link == "" {
			link = strings.TrimSpace(deviceAuth.VerificationURI)
		}
		fmt.Fprintln(output)
		fmt.Fprintln(output, "SSO second-factor confirmation is required before the SSH session can continue.")
		fmt.Fprintf(output, "Open this link in your browser: %s\n", link)
		if strings.TrimSpace(deviceAuth.UserCode) != "" && strings.TrimSpace(deviceAuth.VerificationURIComplete) == "" {
			fmt.Fprintf(output, "Enter this code if prompted: %s\n", deviceAuth.UserCode)
		}
		fmt.Fprintf(output, "Waiting up to %s for confirmation...\n\n", cfg.AuthTimeout.Round(time.Second))
	}

	pollInterval := cfg.PollInterval
	if deviceAuth.Interval > 0 {
		serverSuggested := time.Duration(deviceAuth.Interval) * time.Second
		if serverSuggested > pollInterval {
			pollInterval = serverSuggested
		}
	}
	identity, err := pollForToken(ctx, cfg, discovery.TokenEndpoint, deviceAuth.DeviceCode, pollInterval)
	if err != nil {
		return Identity{}, err
	}

	if output != nil {
		fmt.Fprintln(output, "SSO confirmation successful. Continuing SSH session...")
		fmt.Fprintln(output)
	}
	return identity, nil
}

var (
	clientCache   sync.Map
	clientCacheMu sync.Mutex
)

type cachedHTTPClient struct {
	client   *http.Client
	refCount int
}

func httpClient(cfg Config) *http.Client {
	cacheKey := fmt.Sprintf("%v_%d", cfg.InsecureSkipVerify, cfg.RequestTimeout)

	if cached, ok := clientCache.Load(cacheKey); ok {
		return cached.(*cachedHTTPClient).client
	}

	clientCacheMu.Lock()
	defer clientCacheMu.Unlock()

	if cached, ok := clientCache.Load(cacheKey); ok {
		return cached.(*cachedHTTPClient).client
	}

	timeout := time.Duration(DefaultRequestTimeoutSeconds) * time.Second
	if cfg.RequestTimeout > 0 {
		timeout = cfg.RequestTimeout
	}
	client := &http.Client{Timeout: timeout}
	if cfg.InsecureSkipVerify {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // user-configured for self-signed certs
		}
	}

	clientCache.Store(cacheKey, &cachedHTTPClient{client: client})
	return client
}

func fetchDiscoveryDocument(ctx context.Context, cfg Config) (*discoveryDocument, error) {
	endpoint := issuerURL(cfg) + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build SSO discovery request: %w", err)
	}

	resp, err := httpClient(cfg).Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to load SSO discovery document from %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("SSO discovery request failed with status %s", resp.Status)
	}

	var doc discoveryDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("failed to decode SSO discovery document: %w", err)
	}
	if strings.TrimSpace(doc.DeviceAuthorizationEndpoint) == "" || strings.TrimSpace(doc.TokenEndpoint) == "" {
		return nil, fmt.Errorf("SSO discovery document is missing device or token endpoints")
	}
	return &doc, nil
}

func startDeviceAuthorization(ctx context.Context, cfg Config, endpoint string) (*deviceAuthorizationResponse, error) {
	values := url.Values{}
	values.Set("client_id", cfg.ClientID)
	if strings.TrimSpace(cfg.ClientSecret) != "" {
		values.Set("client_secret", cfg.ClientSecret)
	}
	if strings.TrimSpace(cfg.Scope) != "" {
		values.Set("scope", cfg.Scope)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to build SSO device authorization request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if strings.TrimSpace(cfg.ClientSecret) != "" {
		req.SetBasicAuth(cfg.ClientID, cfg.ClientSecret)
	}

	resp, err := httpClient(cfg).Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to start SSO device authorization: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var tokenErr tokenErrorResponse
		_ = json.NewDecoder(resp.Body).Decode(&tokenErr)
		message := formatOAuthErrorMessage(tokenErr, resp.Status)
		return nil, fmt.Errorf("SSO device authorization failed: %s", message)
	}

	var deviceAuth deviceAuthorizationResponse
	if err := json.NewDecoder(resp.Body).Decode(&deviceAuth); err != nil {
		return nil, fmt.Errorf("failed to decode SSO device authorization response: %w", err)
	}
	if strings.TrimSpace(deviceAuth.DeviceCode) == "" {
		return nil, fmt.Errorf("SSO device authorization response did not include a device_code")
	}
	return &deviceAuth, nil
}

func pollForToken(ctx context.Context, cfg Config, tokenEndpoint, deviceCode string, pollInterval time.Duration) (Identity, error) {
	wait := pollInterval
	for {
		select {
		case <-ctx.Done():
			return Identity{}, fmt.Errorf("SSO confirmation timed out after %s", cfg.AuthTimeout.Round(time.Second))
		case <-time.After(wait):
		}

		values := url.Values{}
		values.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
		values.Set("device_code", deviceCode)
		values.Set("client_id", cfg.ClientID)
		if strings.TrimSpace(cfg.ClientSecret) != "" {
			values.Set("client_secret", cfg.ClientSecret)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(values.Encode()))
		if err != nil {
			return Identity{}, fmt.Errorf("failed to build SSO token polling request: %w", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if strings.TrimSpace(cfg.ClientSecret) != "" {
			req.SetBasicAuth(cfg.ClientID, cfg.ClientSecret)
		}

		resp, err := httpClient(cfg).Do(req)
		if err != nil {
			return Identity{}, fmt.Errorf("failed to poll SSO token endpoint: %w", err)
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			var tokenResp tokenSuccessResponse
			if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
				_ = resp.Body.Close()
				return Identity{}, fmt.Errorf("failed to decode SSO token response: %w", err)
			}
			_ = resp.Body.Close()
			return identityFromTokenResponse(tokenResp), nil
		}

		var tokenErr tokenErrorResponse
		_ = json.NewDecoder(resp.Body).Decode(&tokenErr)
		_ = resp.Body.Close()

		switch tokenErr.Error {
		case "authorization_pending":
			wait = pollInterval
			continue
		case "slow_down":
			wait = pollInterval + 5*time.Second
			continue
		case "expired_token":
			return Identity{}, fmt.Errorf("SSO device code expired before confirmation completed")
		default:
			message := formatOAuthErrorMessage(tokenErr, resp.Status)
			return Identity{}, fmt.Errorf("SSO confirmation failed: %s", message)
		}
	}
}

func identityFromTokenResponse(tokenResp tokenSuccessResponse) Identity {
	if identity := extractIdentityFromJWT(tokenResp.IDToken); identity.BestIdentifier() != "" {
		return identity
	}
	return extractIdentityFromJWT(tokenResp.AccessToken)
}

func extractIdentityFromJWT(token string) Identity {
	token = strings.TrimSpace(token)
	if token == "" {
		return Identity{}
	}

	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return Identity{}
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Identity{}
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Identity{}
	}

	return Identity{
		Subject:           firstStringClaim(claims, "sub"),
		PreferredUsername: firstStringClaim(claims, "preferred_username", "username", "upn"),
		Email:             firstStringClaim(claims, "email"),
	}
}

func firstStringClaim(claims map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		value, ok := claims[key]
		if !ok {
			continue
		}
		if text, ok := value.(string); ok {
			if trimmed := strings.TrimSpace(text); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func formatOAuthErrorMessage(tokenErr tokenErrorResponse, fallback string) string {
	message := strings.TrimSpace(tokenErr.ErrorDescription)
	if message == "" {
		message = strings.TrimSpace(tokenErr.Error)
	}
	if message == "" {
		message = fallback
	}

	switch tokenErr.Error {
	case "invalid_client", "unauthorized_client":
		return message + ". Check `sso.client_id`, set `sso.client_secret` if the Keycloak client is confidential, and enable the OAuth 2.0 Device Authorization Grant for that client"
	default:
		lower := strings.ToLower(message)
		if strings.Contains(lower, "invalid client") || strings.Contains(lower, "invalid client credentials") {
			return message + ". Check `sso.client_id`, set `sso.client_secret` if the Keycloak client is confidential, and enable the OAuth 2.0 Device Authorization Grant for that client"
		}
		return message
	}
}
