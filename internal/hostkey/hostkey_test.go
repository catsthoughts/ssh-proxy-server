package hostkey

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestGeneratesEd25519Key(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "ssh_host_key")

	signer, err := LoadOrGenerateHostKey(keyPath)
	if err != nil {
		t.Fatalf("LoadOrGenerateHostKey() error: %v", err)
	}
	if signer.PublicKey().Type() != ssh.KeyAlgoED25519 {
		t.Fatalf("generated key type = %s, want %s", signer.PublicKey().Type(), ssh.KeyAlgoED25519)
	}

	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("key file missing: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode = %o, want 600", info.Mode().Perm())
	}

	// The generated key must be loadable on the next run.
	reloaded, err := LoadOrGenerateHostKey(keyPath)
	if err != nil {
		t.Fatalf("LoadOrGenerateHostKey() reload error: %v", err)
	}
	if reloaded.PublicKey().Type() != ssh.KeyAlgoED25519 {
		t.Fatalf("reloaded key type = %s, want %s", reloaded.PublicKey().Type(), ssh.KeyAlgoED25519)
	}
}

func TestLoadsExistingRSAKey(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "ssh_host_rsa")

	// Generate an RSA key the way older proxy versions did.
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)}
	keyBytes := pem.EncodeToMemory(block)
	if err := os.WriteFile(keyPath, keyBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		t.Fatal(err)
	}

	reloaded, err := LoadOrGenerateHostKey(keyPath)
	if err != nil {
		t.Fatalf("LoadOrGenerateHostKey() error: %v", err)
	}
	if reloaded.PublicKey().Type() != signer.PublicKey().Type() {
		t.Fatalf("reloaded key type = %s, want %s", reloaded.PublicKey().Type(), signer.PublicKey().Type())
	}
}
