package appconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ssh-proxy-server/internal/recording"
	"ssh-proxy-server/internal/server"
)

func TestLoadAppliesDefaultsAndOverrides(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	configPath := filepath.Join(t.TempDir(), "config.json")
	configJSON := `{
		"listen": "0.0.0.0:2022",
		"allow_direct_commands": true,
		"recording_format": "script"
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatalf("WriteFile() returned error: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.Listen != "0.0.0.0:2022" {
		t.Fatalf("Listen = %q, want %q", cfg.Listen, "0.0.0.0:2022")
	}
	if cfg.Key != "./ssh_host_key" {
		t.Fatalf("Key = %q, want %q", cfg.Key, "./ssh_host_key")
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
	if cfg.RecordingsDir != "./recordings" {
		t.Fatalf("RecordingsDir = %q, want %q", cfg.RecordingsDir, "./recordings")
	}
	if cfg.AuthorizedKeys != filepath.Join(homeDir, ".ssh", "authorized_keys") {
		t.Fatalf("AuthorizedKeys = %q, want %q", cfg.AuthorizedKeys, filepath.Join(homeDir, ".ssh", "authorized_keys"))
	}
	if !cfg.AutoAcceptClientKeys {
		t.Fatal("expected AutoAcceptClientKeys to default to true")
	}
	if !cfg.AllowDirectCommands {
		t.Fatal("expected AllowDirectCommands to be true from config")
	}
	if cfg.RecordingFormat != recording.FormatScript {
		t.Fatalf("RecordingFormat = %q, want %q", cfg.RecordingFormat, recording.FormatScript)
	}
}

func TestLoadRejectsUnsupportedRecordingFormat(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	configPath := filepath.Join(t.TempDir(), "config.json")
	configJSON := `{"recording_format":"unknown"}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatalf("WriteFile() returned error: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected Load() to reject an unsupported recording format")
	}
	if !strings.Contains(err.Error(), "recording format") {
		t.Fatalf("expected recording-format validation error, got %q", err.Error())
	}
}

func TestLoadRejectsMissingAuthorizedKeysWhenAutoAcceptDisabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	configPath := filepath.Join(t.TempDir(), "config.json")
	configJSON := `{
		"auto_accept_client_keys": false,
		"authorized_keys": ""
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatalf("WriteFile() returned error: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected Load() to reject empty authorized_keys when auto_accept_client_keys is false")
	}
	if !strings.Contains(err.Error(), "authorized_keys") {
		t.Fatalf("expected authorized_keys validation error, got %q", err.Error())
	}
}

func TestLoadAppliesStaticRoutingSettings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	configPath := filepath.Join(t.TempDir(), "config.json")
	configJSON := `{
		"retries": 2,
		"connect_timeout_seconds": 7,
		"static_routing": {
			"enabled": true,
			"servers": ["primary.example.com:22", "backup.example.com:2200"],
			"mode": "round_robin"
		}
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatalf("WriteFile() returned error: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if !cfg.StaticRouting.Enabled {
		t.Fatal("expected StaticRouting.Enabled to be true")
	}
	if cfg.StaticRouting.Mode != server.RoutingModeRoundRobin {
		t.Fatalf("StaticRouting.Mode = %q, want %q", cfg.StaticRouting.Mode, server.RoutingModeRoundRobin)
	}
	if cfg.Retries != 2 {
		t.Fatalf("Retries = %d, want %d", cfg.Retries, 2)
	}
	if cfg.ConnectTimeoutSeconds != 7 {
		t.Fatalf("ConnectTimeoutSeconds = %d, want %d", cfg.ConnectTimeoutSeconds, 7)
	}
	if len(cfg.StaticRouting.Servers) != 2 {
		t.Fatalf("StaticRouting.Servers length = %d, want %d", len(cfg.StaticRouting.Servers), 2)
	}
}

func TestLoadSupportsLegacyStaticRoutingRetryFields(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	configPath := filepath.Join(t.TempDir(), "config.json")
	configJSON := `{
		"static_routing": {
			"enabled": true,
			"servers": ["primary.example.com:22"],
			"mode": "failover",
			"retries": 3,
			"connect_timeout_seconds": 9
		}
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatalf("WriteFile() returned error: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.Retries != 3 {
		t.Fatalf("Retries = %d, want %d", cfg.Retries, 3)
	}
	if cfg.ConnectTimeoutSeconds != 9 {
		t.Fatalf("ConnectTimeoutSeconds = %d, want %d", cfg.ConnectTimeoutSeconds, 9)
	}
}

func TestLoadRejectsStaticRoutingWithoutServers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	configPath := filepath.Join(t.TempDir(), "config.json")
	configJSON := `{
		"static_routing": {
			"enabled": true,
			"servers": [],
			"mode": "failover"
		}
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatalf("WriteFile() returned error: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected Load() to reject enabled static routing without any servers")
	}
	if !strings.Contains(err.Error(), "static_routing.servers") {
		t.Fatalf("expected static_routing.servers validation error, got %q", err.Error())
	}
}

func TestLoadAppliesSSOSettings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	configPath := filepath.Join(t.TempDir(), "config.json")
	configJSON := `{
		"sso": {
			"enabled": true,
			"base_url": "http://localhost:8080",
			"realm": "ssh-proxy-server",
			"client_id": "ssh-proxy-server-cli",
			"client_secret": "top-secret",
			"auth_timeout_seconds": 45,
			"poll_interval_seconds": 3,
			"connect_timeout_seconds": 9,
			"enforce_ssh_user_match": false
		}
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatalf("WriteFile() returned error: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if !cfg.SSO.Enabled {
		t.Fatal("expected SSO.Enabled to be true")
	}
	if cfg.SSO.Provider != "keycloak" {
		t.Fatalf("SSO.Provider = %q, want %q", cfg.SSO.Provider, "keycloak")
	}
	if cfg.SSO.Realm != "ssh-proxy-server" {
		t.Fatalf("SSO.Realm = %q, want %q", cfg.SSO.Realm, "ssh-proxy-server")
	}
	if cfg.SSO.ClientSecret != "top-secret" {
		t.Fatalf("SSO.ClientSecret = %q, want %q", cfg.SSO.ClientSecret, "top-secret")
	}
	if cfg.SSO.AuthTimeoutSeconds != 45 {
		t.Fatalf("SSO.AuthTimeoutSeconds = %d, want %d", cfg.SSO.AuthTimeoutSeconds, 45)
	}
	if cfg.SSO.PollIntervalSeconds != 3 {
		t.Fatalf("SSO.PollIntervalSeconds = %d, want %d", cfg.SSO.PollIntervalSeconds, 3)
	}
	if cfg.SSO.ConnectTimeoutSeconds != 9 {
		t.Fatalf("SSO.ConnectTimeoutSeconds = %d, want %d", cfg.SSO.ConnectTimeoutSeconds, 9)
	}
	if cfg.SSO.EnforceSSHUserMatch {
		t.Fatal("expected SSO.EnforceSSHUserMatch to be false from config")
	}
}

func TestLoadRejectsUnsupportedSSOProvider(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	configPath := filepath.Join(t.TempDir(), "config.json")
	configJSON := `{
		"sso": {
			"enabled": true,
			"provider": "unknown"
		}
	}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatalf("WriteFile() returned error: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("expected Load() to reject an unsupported SSO provider")
	}
	if !strings.Contains(err.Error(), "sso.provider") {
		t.Fatalf("expected sso.provider validation error, got %q", err.Error())
	}
}
