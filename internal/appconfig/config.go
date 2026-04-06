package appconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"ssh-proxy-server/internal/recording"
	"ssh-proxy-server/internal/server"
)

const insecureIgnoreHostKeyEnv = "SSH_PROXY_INSECURE_IGNORE_HOSTKEY"

// Config holds the startup configuration loaded from JSON.
type Config struct {
	Listen                string `json:"listen"`
	Key                   string `json:"key"`
	LogLevel              string `json:"log_level"`
	RecordingsDir         string `json:"recordings_dir"`
	AuthorizedKeys        string `json:"authorized_keys"`
	AutoAcceptClientKeys  bool   `json:"auto_accept_client_keys"`
	AllowDirectCommands   bool   `json:"allow_direct_commands"`
	InsecureIgnoreHostKey bool   `json:"insecure_ignore_hostkey"`
	RecordingFormat       string `json:"recording_format"`
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

	cfg := Default()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config %s: %w", configPath, err)
	}

	cfg.RecordingFormat = recording.NormalizeFormat(cfg.RecordingFormat)
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

	return nil
}

func envBool(name string, defaultValue bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return defaultValue
	}
	return value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes")
}
