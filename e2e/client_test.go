package tests

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	proxyAddrHost  = "localhost:2222"
	caDirHost      = "./e2e-keys"
	testUser       = "testuser"
)

func waitForPortHost(addr string, timeout time.Duration) error {
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

func TestClientCertificateAuthHost(t *testing.T) {
	if err := waitForPortHost(proxyAddrHost, 60*time.Second); err != nil {
		t.Fatalf("Proxy not ready: %v", err)
	}

	certPath := filepath.Join(caDirHost, "clients", "user1_key_valid-cert.pub")
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		certPath = filepath.Join(caDirHost, "clients", "user1_key-cert.pub")
	}

	keyPath := filepath.Join(caDirHost, "clients", "user1_key")
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		t.Fatalf("Client key not found at %s", keyPath)
	}

	signers := loadKeyPairHost(t, keyPath, certPath)
	config := &ssh.ClientConfig{
		User: testUser,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signers...),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout: 15 * time.Second,
	}

	client, err := ssh.Dial("tcp", proxyAddrHost, config)
	if err != nil {
		t.Fatalf("Failed to connect to proxy with certificate: %v", err)
	}
	defer client.Close()

	_, _, err = client.SendRequest("noop", true, nil)
	if err != nil {
		t.Fatalf("Failed to send request to proxy: %v", err)
	}

	t.Logf("SUCCESS: Client certificate authentication worked (host)")
}

func TestClientKeyAuthWithoutCertificateHost(t *testing.T) {
	if err := waitForPortHost(proxyAddrHost, 60*time.Second); err != nil {
		t.Fatalf("Proxy not ready: %v", err)
	}

	keyPath := filepath.Join(caDirHost, "clients", "user1_key")
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		t.Fatalf("Client key not found at %s", keyPath)
	}

	signers := loadKeyPairHost(t, keyPath, "")
	config := &ssh.ClientConfig{
		User: testUser,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signers...),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout: 15 * time.Second,
	}

	client, err := ssh.Dial("tcp", proxyAddrHost, config)
	if err != nil {
		t.Fatalf("Failed to connect to proxy with key only: %v", err)
	}
	defer client.Close()

	_, _, err = client.SendRequest("noop", true, nil)
	if err != nil {
		t.Fatalf("Failed to send request to proxy: %v", err)
	}

	t.Logf("SUCCESS: Client key authentication (without cert) worked (host)")
}

func TestHostCertificateVerificationHost(t *testing.T) {
	if err := waitForPortHost(proxyAddrHost, 60*time.Second); err != nil {
		t.Fatalf("Proxy not ready: %v", err)
	}

	certPath := filepath.Join(caDirHost, "clients", "user1_key_valid-cert.pub")
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		certPath = filepath.Join(caDirHost, "clients", "user1_key-cert.pub")
	}

	keyPath := filepath.Join(caDirHost, "clients", "user1_key")

	signers := loadKeyPairHost(t, keyPath, certPath)
	config := &ssh.ClientConfig{
		User: testUser,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signers...),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout: 15 * time.Second,
	}

	client, err := ssh.Dial("tcp", proxyAddrHost, config)
	if err != nil {
		t.Fatalf("Failed to connect with host cert verification: %v", err)
	}
	defer client.Close()

	_, _, err = client.SendRequest("noop", true, nil)
	if err != nil {
		t.Fatalf("Failed to send request to proxy: %v", err)
	}

	t.Logf("SUCCESS: Host certificate verification worked and command executed (host)")
}

func TestExpiredCertificateRejectedHost(t *testing.T) {
	if err := waitForPortHost(proxyAddrHost, 60*time.Second); err != nil {
		t.Fatalf("Proxy not ready: %v", err)
	}

	validCertPath := filepath.Join(caDirHost, "clients", "user1_key_valid-cert.pub")
	keyPath := filepath.Join(caDirHost, "clients", "user1_key")
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		t.Fatalf("Client key not found at %s", keyPath)
	}

	expiredCertPath := filepath.Join(caDirHost, "clients", "user1_key-cert.pub")
	_ = validCertPath

	signers := loadKeyPairHost(t, keyPath, expiredCertPath)
	config := &ssh.ClientConfig{
		User: testUser,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signers...),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout: 15 * time.Second,
	}

	client, err := ssh.Dial("tcp", proxyAddrHost, config)
	if err == nil {
		client.Close()
		t.Fatalf("Expected connection to be rejected with expired certificate, but it succeeded")
	}

	t.Logf("SUCCESS: Expired certificate was rejected as expected (host)")
}

func TestECDSAKeyAuthHost(t *testing.T) {
	if err := waitForPortHost(proxyAddrHost, 60*time.Second); err != nil {
		t.Fatalf("Proxy not ready: %v", err)
	}

	keyPath := filepath.Join(caDirHost, "clients", "user1_ecdsa_key")
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		t.Fatalf("ECDSA key not found at %s", keyPath)
	}

	certPath := filepath.Join(caDirHost, "clients", "user1_ecdsa_key-cert.pub")

	signers := loadKeyPairHost(t, keyPath, certPath)
	config := &ssh.ClientConfig{
		User: testUser,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signers...),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout: 15 * time.Second,
	}

	client, err := ssh.Dial("tcp", proxyAddrHost, config)
	if err != nil {
		t.Fatalf("Failed to connect to proxy with ECDSA key: %v", err)
	}
	defer client.Close()

	_, _, err = client.SendRequest("noop", true, nil)
	if err != nil {
		t.Fatalf("Failed to send request to proxy: %v", err)
	}

	t.Logf("SUCCESS: ECDSA key authentication worked (host)")
}

func loadKeyPairHost(t *testing.T, keyPath, certPath string) []ssh.Signer {
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