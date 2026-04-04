package server

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

const AutoAcceptClientKeysEnv = "SSH_PROXY_AUTO_ACCEPT_CLIENT_KEYS"

func DefaultAuthorizedKeysPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(homeDir) == "" {
		return "./authorized_keys"
	}
	return filepath.Join(homeDir, ".ssh", "authorized_keys")
}

func isAuthorizedClientKey(key ssh.PublicKey, authorizedKeysPath string, autoAcceptClientKeys bool) error {
	if autoAcceptClientKeys {
		return nil
	}

	authorizedKeysPath, err := getAuthorizedKeysPath(authorizedKeysPath)
	if err != nil {
		return err
	}

	keyData, err := os.ReadFile(authorizedKeysPath)
	if err != nil {
		return fmt.Errorf("failed to read authorized keys from %s: %w", authorizedKeysPath, err)
	}

	remaining := keyData
	for len(bytes.TrimSpace(remaining)) > 0 {
		authorizedKey, _, _, rest, err := ssh.ParseAuthorizedKey(remaining)
		if err != nil {
			return fmt.Errorf("failed to parse authorized keys from %s: %w", authorizedKeysPath, err)
		}
		if bytes.Equal(authorizedKey.Marshal(), key.Marshal()) {
			return nil
		}
		remaining = rest
	}

	return fmt.Errorf("unauthorized public key: %s", ssh.FingerprintSHA256(key))
}

func getAuthorizedKeysPath(configuredPath string) (string, error) {
	if path := strings.TrimSpace(configuredPath); path != "" {
		return path, nil
	}

	defaultPath := DefaultAuthorizedKeysPath()
	if _, err := os.Stat(defaultPath); err != nil {
		return "", fmt.Errorf("authorized_keys not found at %s; use -authorized-keys to set an explicit path", defaultPath)
	}

	return defaultPath, nil
}
