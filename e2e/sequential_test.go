//go:build e2econtainer

package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestSequentialUsers(t *testing.T) {
	if err := waitForPort(proxyAddr, 60*time.Second); err != nil {
		t.Fatalf("Proxy not ready: %v", err)
	}

	for numUsers := 1; numUsers <= 5; numUsers++ {
		t.Logf("\n=== Testing with %d user(s) sequentially ===", numUsers)
		for userNum := 1; userNum <= numUsers; userNum++ {
			t.Logf("Running user %d...", userNum)
			result := runSequentialSession(t, userNum)
			if !result.success {
				t.Fatalf("User %d FAILED: %s", userNum, result.errorMsg)
			}
			t.Logf("User %d SUCCEEDED", userNum)
		}
		t.Logf("=== All %d users completed successfully ===\n", numUsers)
	}
}

func runSequentialSession(t *testing.T, userNum int) struct {
	success  bool
	errorMsg string
} {
	certPath := filepath.Join(caDir, "clients", fmt.Sprintf("user%d_key_valid-cert.pub", userNum))
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		certPath = filepath.Join(caDir, "clients", fmt.Sprintf("user%d_key-cert.pub", userNum))
	}

	keyPath := filepath.Join(caDir, "clients", fmt.Sprintf("user%d_key", userNum))
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		return struct {
			success  bool
			errorMsg string
		}{false, fmt.Sprintf("key not found at %s", keyPath)}
	}

	config := &ssh.ClientConfig{
		User: testUser,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(loadKeyPair(t, keyPath, certPath)...),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout: 30 * time.Second,
	}

	client, err := ssh.Dial("tcp", proxyAddr, config)
	if err != nil {
		return struct {
			success  bool
			errorMsg string
		}{false, fmt.Sprintf("dial failed: %v", err)}
	}
	defer client.Close()

	agentChannel, _, err := client.OpenChannel("auth-agent@openssh.com", nil)
	if err != nil {
		return struct {
			success  bool
			errorMsg string
		}{false, fmt.Sprintf("failed to open agent channel: %v", err)}
	}

	go setupAgentWithKeys(t, keyPath, certPath, agentChannel)

	session, err := client.NewSession()
	if err != nil {
		return struct {
			success  bool
			errorMsg string
		}{false, fmt.Sprintf("create session failed: %v", err)}
	}
	defer session.Close()

	command := fmt.Sprintf("echo 'user%d-success'", userNum)
	if err := session.Run(command); err != nil {
		return struct {
			success  bool
			errorMsg string
		}{false, fmt.Sprintf("run command failed: %v", err)}
	}

	return struct {
		success  bool
		errorMsg string
	}{true, ""}
}