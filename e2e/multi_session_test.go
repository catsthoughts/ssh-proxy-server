//go:build e2econtainer

package tests

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	proxyAddrMulti = "proxy:2222"
	caDirMulti     = "/data"
	testUserMulti  = "testuser"
	numSessions    = 10
)

func TestMultipleConcurrentSessions(t *testing.T) {
	if err := waitForPort(proxyAddrMulti, 60*time.Second); err != nil {
		t.Fatalf("Proxy not ready: %v", err)
	}

	t.Logf("Starting 10 single sessions sequentially...")
	for i := 1; i <= numSessions; i++ {
		t.Logf("Running session %d...", i)
		result := runSessionTest(t, i)
		if !result.success {
			t.Logf("Session %d FAILED: %s", result.userNum, result.errorMsg)
			t.Fatalf("Session %d failed: %s", result.userNum, result.errorMsg)
		}
		t.Logf("Session %d SUCCEEDED", result.userNum)
	}

	t.Logf("SUCCESS: All %d sequential sessions completed successfully", numSessions)
}

func TestOneUserRandomConcurrentSessions(t *testing.T) {
	if err := waitForPort(proxyAddrMulti, 60*time.Second); err != nil {
		t.Fatalf("Proxy not ready: %v", err)
	}

	rand.Seed(time.Now().UnixNano())
	var wg sync.WaitGroup
	var sessionCount int
	var mu sync.Mutex

	for userNum := 1; userNum <= numSessions; userNum++ {
		numConcurrentSessions := rand.Intn(5) + 1
		t.Logf("User %d: starting %d concurrent sessions", userNum, numConcurrentSessions)

		for sessionNum := 1; sessionNum <= numConcurrentSessions; sessionNum++ {
			wg.Add(1)
			go func(uNum, sNum int) {
				defer wg.Done()
				result := runSessionTest(t, uNum)
				mu.Lock()
				if !result.success {
					t.Logf("User %d Session %d FAILED: %s", uNum, sNum, result.errorMsg)
				} else {
					t.Logf("User %d Session %d SUCCEEDED", uNum, sNum)
				}
				sessionCount++
				mu.Unlock()
			}(userNum, sessionNum)
		}
	}

	wg.Wait()

	t.Logf("SUCCESS: All %d concurrent sessions for all %d users completed successfully", sessionCount, numSessions)
}

func runSessionTest(t *testing.T, userNum int) struct {
	userNum  int
	success  bool
	errorMsg string
} {
	keyPath := filepath.Join(caDirMulti, "clients", fmt.Sprintf("user%d_key", userNum))
	certPath := filepath.Join(caDirMulti, "clients", fmt.Sprintf("user%d_key_valid-cert.pub", userNum))
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		certPath = filepath.Join(caDirMulti, "clients", fmt.Sprintf("user%d_key-cert.pub", userNum))
	}

	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		return struct {
			userNum  int
			success  bool
			errorMsg string
		}{userNum, false, fmt.Sprintf("key not found at %s", keyPath)}
	}

	config := &ssh.ClientConfig{
		User: testUserMulti,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(loadKeyPairMulti(t, keyPath, certPath)...),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout: 15 * time.Second,
	}

	client, err := ssh.Dial("tcp", proxyAddrMulti, config)
	if err != nil {
		return struct {
			userNum  int
			success  bool
			errorMsg string
		}{userNum, false, fmt.Sprintf("dial failed: %v", err)}
	}
	defer client.Close()

	agentChannel, requests, err := client.OpenChannel("auth-agent@openssh.com", nil)
	if err != nil {
		return struct {
			userNum  int
			success  bool
			errorMsg string
		}{userNum, false, fmt.Sprintf("failed to open agent channel: %v", err)}
	}
	go ssh.DiscardRequests(requests)
	go setupAgentWithKeys(t, keyPath, certPath, agentChannel)

	session, err := client.NewSession()
	if err != nil {
		return struct {
			userNum  int
			success  bool
			errorMsg string
		}{userNum, false, fmt.Sprintf("create session failed: %v", err)}
	}
	defer session.Close()

	sleepDuration := rand.Intn(5) + 1
	time.Sleep(time.Duration(sleepDuration) * time.Second)

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	expectedOutput := fmt.Sprintf("session-%d", userNum)
	command := fmt.Sprintf("echo 'session-%d'", userNum)

	if err := session.Run(command); err != nil {
		return struct {
			userNum  int
			success  bool
			errorMsg string
		}{userNum, false, fmt.Sprintf("run command failed: %v (stderr: %s)", err, stderr.String())}
	}

	if !strings.Contains(stdout.String(), expectedOutput) {
		return struct {
			userNum  int
			success  bool
			errorMsg string
		}{userNum, false, fmt.Sprintf("unexpected output: %s", stdout.String())}
	}

	return struct {
		userNum  int
		success  bool
		errorMsg string
	}{userNum, true, ""}
}

func loadKeyPairMulti(t *testing.T, keyPath, certPath string) []ssh.Signer {
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