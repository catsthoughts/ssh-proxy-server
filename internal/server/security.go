package server

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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

func isAuthorizedClientKeyWithCA(key ssh.PublicKey, authorizedKeysPath string, trustedCACerts []ssh.PublicKey, autoAcceptClientKeys bool) error {
	if autoAcceptClientKeys {
		return nil
	}

	if cert, ok := key.(*ssh.Certificate); ok {
		for _, ca := range trustedCACerts {
			if bytes.Equal(cert.SignatureKey.Marshal(), ca.Marshal()) {
				if cert.ValidBefore == 0 || cert.ValidBefore > uint64(time.Now().Unix()) {
					return nil
				}
				return fmt.Errorf("certificate is expired")
			}
		}
		if len(trustedCACerts) > 0 {
			return fmt.Errorf("certificate signed by unknown CA: %s", ssh.FingerprintSHA256(key))
		}
	}

	return isAuthorizedClientKey(key, authorizedKeysPath, false)
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

func LoadTrustedCACerts(caCertPaths []string) ([]ssh.PublicKey, error) {
	var certs []ssh.PublicKey
	for _, path := range caCertPaths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA cert from %s: %w", path, err)
		}
		for len(bytes.TrimSpace(data)) > 0 {
			cert, _, _, rest, err := ssh.ParseAuthorizedKey(data)
			if err != nil {
				return nil, fmt.Errorf("failed to parse CA cert from %s: %w", path, err)
			}
			certs = append(certs, cert)
			data = rest
		}
	}
	return certs, nil
}

func LoadCAPublicKeyFromFile(path string) (ssh.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA key from %s: %w", path, err)
	}

	pub, _, _, rest, err := ssh.ParseAuthorizedKey(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CA key from %s: %w", path, err)
	}
	if len(bytes.TrimSpace(rest)) > 0 {
		return nil, fmt.Errorf("CA key file %s contains multiple keys, expected single key", path)
	}

	return pub, nil
}
