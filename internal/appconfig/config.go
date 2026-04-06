package appconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"ssh-proxy-server/internal/recording"
	"ssh-proxy-server/internal/server"
	"ssh-proxy-server/internal/sso"
)

const insecureIgnoreHostKeyEnv = "SSH_PROXY_INSECURE_IGNORE_HOSTKEY"

type StaticRoutingConfig struct {
	Enabled bool     `json:"enabled"`
	Servers []string `json:"servers"`
	Mode    string   `json:"mode"`

	// Deprecated: kept for backward compatibility with older configs.
	Retries               int `json:"retries"`
	ConnectTimeoutSeconds int `json:"connect_timeout_seconds"`
}

type SSOConfig struct {
	Enabled               bool   `json:"enabled"`
	Provider              string `json:"provider"`
	BaseURL               string `json:"base_url"`
	Realm                 string `json:"realm"`
	ClientID              string `json:"client_id"`
	ClientSecret          string `json:"client_secret"`
	Scope                 string `json:"scope"`
	AuthTimeoutSeconds    int    `json:"auth_timeout_seconds"`
	PollIntervalSeconds   int    `json:"poll_interval_seconds"`
	ConnectTimeoutSeconds int    `json:"connect_timeout_seconds"`
	EnforceSSHUserMatch   bool   `json:"enforce_ssh_user_match"`
}

// Config holds the startup configuration loaded from JSON.
type Config struct {
	Listen                string              `json:"listen"`
	Key                   string              `json:"key"`
	LogLevel              string              `json:"log_level"`
	RecordingsDir         string              `json:"recordings_dir"`
	AuthorizedKeys        string              `json:"authorized_keys"`
	AutoAcceptClientKeys  bool                `json:"auto_accept_client_keys"`
	AllowDirectCommands   bool                `json:"allow_direct_commands"`
	InsecureIgnoreHostKey bool                `json:"insecure_ignore_hostkey"`
	RecordingFormat       string              `json:"recording_format"`
	Retries               int                 `json:"retries"`
	ConnectTimeoutSeconds int                 `json:"connect_timeout_seconds"`
	StaticRouting         StaticRoutingConfig `json:"static_routing"`
	SSO                   SSOConfig           `json:"sso"`
}

// Default returns the default application configuration.
func Default() Config {
	return Config{
		Listen:                "localhost:2222",
		Key:                   "./ssh_host_key",
		LogLevel:              "info",
		RecordingsDir:         "./recordings",
		AuthorizedKeys:        server.DefaultAuthorizedKeysPath(),
		AutoAcceptClientKeys:  envBool(server.AutoAcceptClientKeysEnv, true),
		AllowDirectCommands:   false,
		InsecureIgnoreHostKey: envBool(insecureIgnoreHostKeyEnv, false),
		RecordingFormat:       recording.FormatAsciinema,
		Retries:               0,
		ConnectTimeoutSeconds: server.DefaultConnectTimeoutSeconds,
		StaticRouting: StaticRoutingConfig{
			Enabled: false,
			Servers: nil,
			Mode:    server.RoutingModeFailover,
		},
		SSO: SSOConfig{
			Enabled:               false,
			Provider:              sso.DefaultProvider,
			BaseURL:               sso.DefaultBaseURL,
			Realm:                 sso.DefaultRealm,
			ClientID:              sso.DefaultClientID,
			Scope:                 sso.DefaultScope,
			AuthTimeoutSeconds:    sso.DefaultAuthTimeoutSeconds,
			PollIntervalSeconds:   sso.DefaultPollIntervalSeconds,
			ConnectTimeoutSeconds: sso.DefaultRequestTimeoutSeconds,
			EnforceSSHUserMatch:   true,
		},
	}
}

// Load reads and validates a JSON configuration file.
func Load(configPath string) (*Config, error) {
	if strings.TrimSpace(configPath) == "" {
		return nil, fmt.Errorf("config path is required")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config %s: %w", configPath, err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse config %s: %w", configPath, err)
	}

	cfg := Default()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config %s: %w", configPath, err)
	}

	cfg.RecordingFormat = recording.NormalizeFormat(cfg.RecordingFormat)
	cfg.StaticRouting.Mode = server.NormalizeRoutingMode(cfg.StaticRouting.Mode)
	cfg.applyLegacyRoutingFallbacks(raw)
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config %s: %w", configPath, err)
	}

	return &cfg, nil
}

// Validate checks that the loaded configuration is usable.
func (c *Config) Validate() error {
	if strings.TrimSpace(c.Listen) == "" {
		return fmt.Errorf("listen is required")
	}
	if strings.TrimSpace(c.Key) == "" {
		return fmt.Errorf("key is required")
	}

	c.LogLevel = strings.TrimSpace(c.LogLevel)
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}
	switch c.LogLevel {
	case "error", "info", "debug":
	default:
		return fmt.Errorf("log_level must be one of: error, info, debug")
	}

	if strings.TrimSpace(c.RecordingsDir) == "" {
		c.RecordingsDir = "./recordings"
	}
	if !c.AutoAcceptClientKeys && strings.TrimSpace(c.AuthorizedKeys) == "" {
		return fmt.Errorf("authorized_keys is required when auto_accept_client_keys is false")
	}
	if !recording.IsSupportedFormat(c.RecordingFormat) {
		return fmt.Errorf("recording format must be %q or %q", recording.FormatAsciinema, recording.FormatScript)
	}
	if c.Retries < 0 {
		return fmt.Errorf("retries must be greater than or equal to 0")
	}
	if c.ConnectTimeoutSeconds <= 0 {
		c.ConnectTimeoutSeconds = server.DefaultConnectTimeoutSeconds
	}

	c.SSO.Provider = sso.NormalizeProvider(c.SSO.Provider)
	if !sso.IsSupportedProvider(c.SSO.Provider) {
		return fmt.Errorf("sso.provider must be %q", sso.DefaultProvider)
	}
	if strings.TrimSpace(c.SSO.BaseURL) == "" {
		c.SSO.BaseURL = sso.DefaultBaseURL
	}
	if strings.TrimSpace(c.SSO.Realm) == "" {
		c.SSO.Realm = sso.DefaultRealm
	}
	if strings.TrimSpace(c.SSO.ClientID) == "" {
		c.SSO.ClientID = sso.DefaultClientID
	}
	if strings.TrimSpace(c.SSO.Scope) == "" {
		c.SSO.Scope = sso.DefaultScope
	}
	if c.SSO.AuthTimeoutSeconds <= 0 {
		c.SSO.AuthTimeoutSeconds = sso.DefaultAuthTimeoutSeconds
	}
	if c.SSO.PollIntervalSeconds <= 0 {
		c.SSO.PollIntervalSeconds = sso.DefaultPollIntervalSeconds
	}
	if c.SSO.ConnectTimeoutSeconds <= 0 {
		c.SSO.ConnectTimeoutSeconds = sso.DefaultRequestTimeoutSeconds
	}

	c.StaticRouting.Mode = server.NormalizeRoutingMode(c.StaticRouting.Mode)
	if !server.IsSupportedRoutingMode(c.StaticRouting.Mode) {
		return fmt.Errorf("static_routing.mode must be %q or %q", server.RoutingModeFailover, server.RoutingModeRoundRobin)
	}
	for i, target := range c.StaticRouting.Servers {
		target = strings.TrimSpace(target)
		if target == "" {
			return fmt.Errorf("static_routing.servers[%d] is empty", i)
		}
		if err := server.ValidateTargetAddress(target); err != nil {
			return fmt.Errorf("static_routing.servers[%d] is invalid: %w", i, err)
		}
		c.StaticRouting.Servers[i] = target
	}
	if c.StaticRouting.Enabled && len(c.StaticRouting.Servers) == 0 {
		return fmt.Errorf("static_routing.servers must contain at least one target when static routing is enabled")
	}

	return nil
}

func (c *Config) applyLegacyRoutingFallbacks(raw map[string]json.RawMessage) {
	if _, ok := raw["retries"]; !ok && c.StaticRouting.Retries > 0 {
		c.Retries = c.StaticRouting.Retries
	}
	if _, ok := raw["connect_timeout_seconds"]; !ok && c.StaticRouting.ConnectTimeoutSeconds > 0 {
		c.ConnectTimeoutSeconds = c.StaticRouting.ConnectTimeoutSeconds
	}
}

func envBool(name string, defaultValue bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return defaultValue
	}
	return value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes")
}
