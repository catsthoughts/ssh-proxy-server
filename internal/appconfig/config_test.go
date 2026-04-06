package appconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ssh-proxy-server/internal/recording"
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
