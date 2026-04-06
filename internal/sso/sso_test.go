package sso

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStartDeviceAuthorizationIncludesClientSecretWhenConfigured(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() returned error: %v", err)
		}
		if got := r.Form.Get("client_id"); got != "ssh-proxy-server" {
			t.Fatalf("client_id = %q, want %q", got, "ssh-proxy-server")
		}
		if got := r.Form.Get("client_secret"); got != "super-secret" {
			t.Fatalf("client_secret = %q, want %q", got, "super-secret")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"device_code":"code-123","verification_uri":"https://sso.example/verify"}`))
	}))
	defer server.Close()

	resp, err := startDeviceAuthorization(context.Background(), Config{
		ClientID:     "ssh-proxy-server",
		ClientSecret: "super-secret",
		Scope:        DefaultScope,
	}, server.URL)
	if err != nil {
		t.Fatalf("startDeviceAuthorization() returned error: %v", err)
	}
	if resp.DeviceCode != "code-123" {
		t.Fatalf("DeviceCode = %q, want %q", resp.DeviceCode, "code-123")
	}
}

func TestStartDeviceAuthorizationReturnsHelpfulInvalidClientError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"Invalid client or Invalid client credentials"}`))
	}))
	defer server.Close()

	_, err := startDeviceAuthorization(context.Background(), Config{ClientID: "ssh-proxy-server"}, server.URL)
	if err == nil {
		t.Fatal("expected startDeviceAuthorization() to return an invalid-client error")
	}
	if !strings.Contains(err.Error(), "sso.client_id") || !strings.Contains(err.Error(), "sso.client_secret") {
		t.Fatalf("expected helpful invalid-client guidance, got %q", err.Error())
	}
}

func TestPollForTokenUsesConfiguredConnectTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
	}))
	defer server.Close()

	err := pollForToken(context.Background(), Config{
		ClientID:       "ssh-proxy-server",
		AuthTimeout:    2 * time.Second,
		RequestTimeout: 50 * time.Millisecond,
	}, server.URL, "device-code", 1*time.Millisecond)
	if err == nil {
		t.Fatal("expected pollForToken() to fail when the HTTP request exceeds the configured connect timeout")
	}
	if !strings.Contains(err.Error(), "failed to poll SSO token endpoint") {
		t.Fatalf("expected timeout-related polling error, got %q", err.Error())
	}
}
