package server

import (
	"crypto/rand"
	"crypto/rsa"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestIsAuthorizedClientKey(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "authorized_keys")
	allowedKey := mustPublicKey(t)
	otherKey := mustPublicKey(t)

	if err := os.WriteFile(keyPath, ssh.MarshalAuthorizedKey(allowedKey), 0o600); err != nil {
		t.Fatalf("WriteFile() returned error: %v", err)
	}

	if err := isAuthorizedClientKey(allowedKey, keyPath, false); err != nil {
		t.Fatalf("isAuthorizedClientKey() rejected allowed key: %v", err)
	}
	if err := isAuthorizedClientKey(otherKey, keyPath, false); err == nil {
		t.Fatal("isAuthorizedClientKey() accepted an unauthorized key")
	}
}

func TestIsAuthorizedClientKeyAutoAcceptEnabledByDefault(t *testing.T) {
	if err := isAuthorizedClientKey(mustPublicKey(t), "", true); err != nil {
		t.Fatalf("isAuthorizedClientKey() should accept keys when auto-accept is enabled, got: %v", err)
	}
}

func mustPublicKey(t *testing.T) ssh.PublicKey {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() returned error: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("NewSignerFromKey() returned error: %v", err)
	}
	return signer.PublicKey()
}
