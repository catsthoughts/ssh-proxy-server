package appconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const baseConfig = `{
  "listen": "localhost:2222",
  "key": "./ssh_host_key"
}`

func TestLoadResolvesClientSecretFromEnv(t *testing.T) {
	t.Setenv("SSH_PROXY_TEST_SSO_SECRET", "env-secret")
	path := writeConfig(t, `{
  "listen": "localhost:2222",
  "key": "./ssh_host_key",
  "sso": {"enabled": true, "client_secret": {"$env": "SSH_PROXY_TEST_SSO_SECRET"}}
}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.SSO.ClientSecret != "env-secret" {
		t.Fatalf("ClientSecret = %q, want env-secret", cfg.SSO.ClientSecret)
	}
}

func TestLoadResolvesClientSecretFromFile(t *testing.T) {
	dir := t.TempDir()
	secretPath := filepath.Join(dir, "sso_client_secret")
	if err := os.WriteFile(secretPath, []byte("file-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := `{
  "listen": "localhost:2222",
  "key": "./ssh_host_key",
  "sso": {"enabled": true, "client_secret": {"$file": "` + secretPath + `"}}
}`
	path := writeConfig(t, config)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.SSO.ClientSecret != "file-secret" {
		t.Fatalf("ClientSecret = %q, want file-secret", cfg.SSO.ClientSecret)
	}
}

func TestLoadResolvesClientSecretStringPrefix(t *testing.T) {
	t.Setenv("SSH_PROXY_TEST_SSO_SECRET", "env-secret")
	path := writeConfig(t, `{
  "listen": "localhost:2222",
  "key": "./ssh_host_key",
  "sso": {"enabled": true, "client_secret": "$env:SSH_PROXY_TEST_SSO_SECRET"}
}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.SSO.ClientSecret != "env-secret" {
		t.Fatalf("ClientSecret = %q, want env-secret", cfg.SSO.ClientSecret)
	}
}

func TestLoadFailsOnMissingSecretReference(t *testing.T) {
	path := writeConfig(t, `{
  "listen": "localhost:2222",
  "key": "./ssh_host_key",
  "sso": {"enabled": true, "client_secret": {"$env": "SSH_PROXY_DEFINITELY_MISSING"}}
}`)
	if _, err := Load(path); err == nil {
		t.Fatal("Load() expected error for missing $env secret reference")
	}
}

func TestLoadKeepsPlainClientSecret(t *testing.T) {
	path := writeConfig(t, `{
  "listen": "localhost:2222",
  "key": "./ssh_host_key",
  "sso": {"enabled": true, "client_secret": "plain-secret"}
}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.SSO.ClientSecret != "plain-secret" {
		t.Fatalf("ClientSecret = %q, want plain-secret", cfg.SSO.ClientSecret)
	}
}

func TestLoadAccessControlValidation(t *testing.T) {
	valid := writeConfig(t, `{
  "listen": "localhost:2222",
  "key": "./ssh_host_key",
  "access_control": {
    "enabled": true,
    "default": "deny",
    "rules": [{"user": "alice", "targets": ["web-*.example.com"]}]
  }
}`)
	cfg, err := Load(valid)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if !cfg.AccessControl.Enabled || cfg.AccessControl.Default != "deny" || len(cfg.AccessControl.Rules) != 1 {
		t.Fatalf("unexpected access_control config: %+v", cfg.AccessControl)
	}

	invalid := writeConfig(t, `{
  "listen": "localhost:2222",
  "key": "./ssh_host_key",
  "access_control": {"enabled": true, "default": "wrong"}
}`)
	if _, err := Load(invalid); err == nil {
		t.Fatal("Load() expected error for invalid access_control.default")
	}
}

func TestLoadNewSectionsDefaults(t *testing.T) {
	path := writeConfig(t, baseConfig)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.AccessControl.Default != "deny" {
		t.Fatalf("access_control.default = %q, want deny", cfg.AccessControl.Default)
	}
	if cfg.Health.Listen != "127.0.0.1:9091" || cfg.Health.Path != "/healthz" {
		t.Fatalf("unexpected health defaults: %+v", cfg.Health)
	}
	if cfg.RateLimit.MaxPerMinute != 30 {
		t.Fatalf("rate_limit.max_per_minute = %d, want 30", cfg.RateLimit.MaxPerMinute)
	}
}

func TestLoadNewSectionsValidation(t *testing.T) {
	if _, err := Load(writeConfig(t, `{"listen":"l","key":"k","rate_limit":{"enabled":true,"max_per_minute":-1}}`)); err == nil {
		t.Fatal("Load() expected error for negative rate_limit.max_per_minute")
	}
	if _, err := Load(writeConfig(t, `{"listen":"l","key":"k","recordings":{"retention_days":-1}}`)); err == nil {
		t.Fatal("Load() expected error for negative recordings.retention_days")
	}
}

func TestLoadRequiredGroups(t *testing.T) {
	path := writeConfig(t, `{
  "listen": "localhost:2222",
  "key": "./ssh_host_key",
  "sso": {"enabled": true, "required_groups": ["ssh-users", "admins"]}
}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(cfg.SSO.RequiredGroups) != 2 || cfg.SSO.RequiredGroups[0] != "ssh-users" {
		t.Fatalf("RequiredGroups = %v, want [ssh-users admins]", cfg.SSO.RequiredGroups)
	}
}

func TestLoadKnownHostsAndSFTPEnabled(t *testing.T) {
	path := writeConfig(t, `{
  "listen": "localhost:2222",
  "key": "./ssh_host_key",
  "known_hosts": "/etc/ssh-proxy/known_hosts",
  "sftp_enabled": true
}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.KnownHosts != "/etc/ssh-proxy/known_hosts" {
		t.Fatalf("KnownHosts = %q", cfg.KnownHosts)
	}
	if !cfg.SFTPEnabled {
		t.Fatal("SFTPEnabled should be true")
	}
}
