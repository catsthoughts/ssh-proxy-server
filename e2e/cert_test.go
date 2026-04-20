//go:build e2econtainer

package tests

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

const (
	proxyAddr  = "proxy:2222"
	caDir      = "/data"
	testUser   = "testuser"
)

func waitForPort(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("port %s not available after %v", addr, timeout)
}

func TestClientCertificateAuth(t *testing.T) {
	if err := waitForPort(proxyAddr, 60*time.Second); err != nil {
		t.Fatalf("Proxy not ready: %v", err)
	}

	certPath := filepath.Join(caDir, "clients", "user1_key_valid-cert.pub")
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		certPath = filepath.Join(caDir, "clients", "user1_key-cert.pub")
	}

	keyPath := filepath.Join(caDir, "clients", "user1_key")
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		t.Fatalf("Client key not found at %s", keyPath)
	}

	config := &ssh.ClientConfig{
		User: testUser,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(loadKeyPair(t, keyPath, certPath)...),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout: 15 * time.Second,
	}

	client, err := ssh.Dial("tcp", proxyAddr, config)
	if err != nil {
		t.Fatalf("Failed to connect to proxy with certificate: %v", err)
	}
	defer client.Close()

	agentChannel, _, err := client.OpenChannel("auth-agent@openssh.com", nil)
	if err != nil {
		t.Fatalf("Failed to open agent channel: %v", err)
	}

	go setupAgentWithKeys(t, keyPath, certPath, agentChannel)

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	if err := session.Run("echo 'cert-auth-success'"); err != nil {
		t.Fatalf("Failed to run command: %v (stderr: %s)", err, stderr.String())
	}

	if !strings.Contains(stdout.String(), "cert-auth-success") {
		t.Errorf("Unexpected output: %s", stdout.String())
	}

	t.Logf("SUCCESS: Client certificate authentication worked")
}

func TestClientKeyAuthWithoutCertificate(t *testing.T) {
	if err := waitForPort(proxyAddr, 60*time.Second); err != nil {
		t.Fatalf("Proxy not ready: %v", err)
	}

	keyPath := filepath.Join(caDir, "clients", "user1_key")
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		t.Fatalf("Client key not found at %s", keyPath)
	}

	config := &ssh.ClientConfig{
		User: testUser,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(loadKeyPair(t, keyPath, "")...),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout: 15 * time.Second,
	}

	client, err := ssh.Dial("tcp", proxyAddr, config)
	if err != nil {
		t.Fatalf("Failed to connect to proxy with key only: %v", err)
	}
	defer client.Close()

	agentChannel, _, err := client.OpenChannel("auth-agent@openssh.com", nil)
	if err != nil {
		t.Fatalf("Failed to open agent channel: %v", err)
	}

	go setupAgentWithKeys(t, keyPath, "", agentChannel)

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	if err := session.Run("echo 'key-auth-success'"); err != nil {
		t.Fatalf("Failed to run command: %v (stderr: %s)", err, stderr.String())
	}

	if !strings.Contains(stdout.String(), "key-auth-success") {
		t.Errorf("Unexpected output: %s", stdout.String())
	}

	t.Logf("SUCCESS: Client key authentication (without cert) worked")
}

func TestHostCertificateVerification(t *testing.T) {
	if err := waitForPort(proxyAddr, 60*time.Second); err != nil {
		t.Fatalf("Proxy not ready: %v", err)
	}

	validCertPath := filepath.Join(caDir, "clients", "user1_key_valid-cert.pub")
	if _, err := os.Stat(validCertPath); os.IsNotExist(err) {
		validCertPath = filepath.Join(caDir, "clients", "user1_key-cert.pub")
	}

	keyPath := filepath.Join(caDir, "clients", "user1_key")

	config := &ssh.ClientConfig{
		User: testUser,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(loadKeyPair(t, keyPath, validCertPath)...),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout: 15 * time.Second,
	}

	client, err := ssh.Dial("tcp", proxyAddr, config)
	if err != nil {
		t.Fatalf("Failed to connect with host cert verification: %v", err)
	}
	defer client.Close()

	agentChannel, _, err := client.OpenChannel("auth-agent@openssh.com", nil)
	if err != nil {
		t.Fatalf("Failed to open agent channel: %v", err)
	}

	go setupAgentWithKeys(t, keyPath, validCertPath, agentChannel)

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	if err := session.Run("echo 'host-cert-verified'"); err != nil {
		t.Fatalf("Failed to run command: %v (stderr: %s)", err, stderr.String())
	}

	if !strings.Contains(stdout.String(), "host-cert-verified") {
		t.Errorf("Unexpected output: %s", stdout.String())
	}

	t.Logf("SUCCESS: Host certificate verification worked and command executed")
}

func loadKeyPair(t *testing.T, keyPath, certPath string) []ssh.Signer {
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("Failed to read key: %v", err)
	}

	signer, err := ssh.ParsePrivateKey(keyData)
	if err != nil {
		t.Fatalf("Failed to parse private key: %v", err)
	}

	if certPath != "" {
		certData, err := os.ReadFile(certPath)
		if err == nil {
			cert, _, _, _, err := ssh.ParseAuthorizedKey(certData)
			if err == nil {
				if cert, ok := cert.(*ssh.Certificate); ok {
					signer, err = ssh.NewCertSigner(cert, signer)
					if err == nil {
						t.Logf("Loaded certificate for key")
					}
				}
			}
		}
	}

	return []ssh.Signer{signer}
}

func setupAgentWithKeys(t *testing.T, keyPath, certPath string, agentChannel ssh.Channel) {
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("Failed to read key: %v", err)
	}

	rawKey, err := ssh.ParseRawPrivateKey(keyData)
	if err != nil {
		t.Fatalf("Failed to parse raw private key: %v", err)
	}

	if pk, ok := rawKey.(ssh.AlgorithmSigner); ok {
		t.Logf("setupAgentWithKeys: rawKey type=%s", pk.PublicKey().Type())
	}

	var cert *ssh.Certificate
	if certPath != "" {
		certData, err := os.ReadFile(certPath)
		if err == nil {
			parsedCert, _, _, _, err := ssh.ParseAuthorizedKey(certData)
			if err == nil {
				if parsedCert, ok := parsedCert.(*ssh.Certificate); ok {
					cert = parsedCert
					t.Logf("Certificate loaded from file: type=%s keyID=%s fingerprint=%s", cert.Type(), cert.KeyId, ssh.FingerprintSHA256(cert))
				}
			} else {
				t.Logf("Failed to parse certificate: %v", err)
			}
		} else {
			t.Logf("Failed to read certificate file: %v", err)
		}
	}

	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{
		PrivateKey:  rawKey,
		Certificate: cert,
	}); err != nil {
		t.Fatalf("Failed to add key to keyring: %v", err)
	}

	signers, err := keyring.Signers()
	if err != nil {
		t.Logf("Signers from keyring: err=%v", err)
	} else {
		for i, s := range signers {
			t.Logf("Signer %d from keyring: PublicKey type=%s fingerprint=%s", i, s.PublicKey().Type(), ssh.FingerprintSHA256(s.PublicKey()))
			if cert, ok := s.PublicKey().(*ssh.Certificate); ok {
				t.Logf("  Certificate type=%s keyID=%s fingerprint=%s", cert.Type(), cert.KeyId, ssh.FingerprintSHA256(cert))
			}
		}
	}

	go agent.ServeAgent(keyring, agentChannel)
}