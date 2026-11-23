// Package secrets implements secret indirection so sensitive config values
// (for example sso.client_secret) never have to be stored as plaintext in the
// canonical config file. Supported forms:
//
//	{"$env": "VAR_NAME"}}          - read from an environment variable
//	{"$file": "/path/to/secret"}   - read from a file (trimmed)
//	"$env:VAR_NAME"                - string prefix form
//	"$file:/path/to/secret"        - string prefix form
//	"anything-else"                - returned as-is
package secrets

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	// EnvPrefix references an environment variable in string form.
	EnvPrefix = "$env:"
	// FilePrefix references a file path in string form.
	FilePrefix = "$file:"
)

// ResolveString resolves a plain string value that may use the "$env:" or
// "$file:" prefix forms. Values without a known prefix are returned unchanged.
func ResolveString(value string) (string, error) {
	value = strings.TrimSpace(value)
	switch {
	case strings.HasPrefix(value, EnvPrefix):
		return ResolveEnv(strings.TrimPrefix(value, EnvPrefix))
	case strings.HasPrefix(value, FilePrefix):
		return ResolveFile(strings.TrimPrefix(value, FilePrefix))
	default:
		return value, nil
	}
}

// ResolveEnv reads a secret from an environment variable. An empty or unset
// variable is treated as an error so misconfiguration is detected early.
func ResolveEnv(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("secret reference $env has an empty variable name")
	}
	value, ok := os.LookupEnv(name)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("environment variable %s referenced by $env is not set or empty", name)
	}
	return value, nil
}

// ResolveFile reads a secret from a file, trimming surrounding whitespace
// (including the trailing newline typical for mounted secret files).
func ResolveFile(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("secret reference $file has an empty path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read secret file %s referenced by $file: %w", path, err)
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", fmt.Errorf("secret file %s referenced by $file is empty", path)
	}
	return value, nil
}

// ResolveJSON resolves a config value that may be either a plain JSON string
// or a JSON object of the form {"$env": "..."} / {"$file": "..."}.
// The returned flag reports whether the value was a secret reference.
func ResolveJSON(raw json.RawMessage) (string, bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false, nil
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		resolved, err := ResolveString(asString)
		if err != nil {
			return "", false, err
		}
		return resolved, strings.HasPrefix(strings.TrimSpace(asString), "$"), nil
	}

	var asObject map[string]json.RawMessage
	if err := json.Unmarshal(raw, &asObject); err != nil {
		return "", false, fmt.Errorf("secret value must be a string or an object with $env/$file: %w", err)
	}

	if envRaw, ok := asObject["$env"]; ok {
		var name string
		if err := json.Unmarshal(envRaw, &name); err != nil {
			return "", false, fmt.Errorf("invalid $env secret reference: %w", err)
		}
		value, err := ResolveEnv(name)
		return value, true, err
	}
	if fileRaw, ok := asObject["$file"]; ok {
		var path string
		if err := json.Unmarshal(fileRaw, &path); err != nil {
			return "", false, fmt.Errorf("invalid $file secret reference: %w", err)
		}
		value, err := ResolveFile(path)
		return value, true, err
	}

	return "", false, fmt.Errorf("secret object must contain exactly one of $env or $file")
}
