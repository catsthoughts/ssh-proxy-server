//go:build !e2econtainer

package tests

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

const (
	aclStaticProxyAddr = "localhost:2222"
	aclEnvProxyAddr    = "localhost:2223"
	aclKeysDir         = "./e2e-keys"
)

// TestACLStaticRoutingAllowedHost checks the ACL enforcement point in
// connectToFirstAvailableTarget (static routing): alice is allowed to reach
// the static target testuser@target-host:22.
func TestACLStaticRoutingAllowedHost(t *testing.T) {
	if err := waitForPortHost(aclStaticProxyAddr, 60*time.Second); err != nil {
		t.Fatalf("Proxy not ready: %v", err)
	}

	client := dialACLProxy(t, aclStaticProxyAddr, "alice", "user2_key")
	defer client.Close()
	serveACLAgent(t, client, "user2_key")

	stdout := runACLCommand(t, client, "echo 'acl-static-allowed'")
	if !strings.Contains(stdout, "acl-static-allowed") {
		t.Fatalf("unexpected command output: %q", stdout)
	}
	t.Logf("SUCCESS: alice was allowed to the static routing target (host)")
}

// TestACLStaticRoutingDeniedHost checks the same enforcement point for a
// denied user: bob has no ACL rule, so the static target must be refused.
func TestACLStaticRoutingDeniedHost(t *testing.T) {
	if err := waitForPortHost(aclStaticProxyAddr, 60*time.Second); err != nil {
		t.Fatalf("Proxy not ready: %v", err)
	}

	client := dialACLProxy(t, aclStaticProxyAddr, "bob", "user3_key")
	defer client.Close()
	serveACLAgent(t, client, "user3_key")

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	if err := session.Run("echo 'should-not-run'"); err == nil {
		t.Fatal("expected command to be denied by access_control, but it succeeded")
	}
	t.Logf("SUCCESS: bob was denied the static routing target (host)")
}

// TestACLEnvRoutingAllowedHost checks the ACL enforcement point in
// handleEnvRequest (LC_SSH_SERVER): alice may route to testuser@target-host:22.
func TestACLEnvRoutingAllowedHost(t *testing.T) {
	if err := waitForPortHost(aclEnvProxyAddr, 60*time.Second); err != nil {
		t.Fatalf("ACL proxy not ready: %v", err)
	}

	client := dialACLProxy(t, aclEnvProxyAddr, "alice", "user2_key")
	defer client.Close()
	serveACLAgent(t, client, "user2_key")

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	if err := session.Setenv("LC_SSH_SERVER", "testuser@target-host:22"); err != nil {
		t.Fatalf("LC_SSH_SERVER was rejected for allowed user alice: %v", err)
	}

	var stdout bytes.Buffer
	session.Stdout = &stdout
	if err := session.Run("echo 'acl-env-allowed'"); err != nil {
		t.Fatalf("command failed for allowed user alice: %v", err)
	}
	if !strings.Contains(stdout.String(), "acl-env-allowed") {
		t.Fatalf("unexpected command output: %q", stdout.String())
	}
	t.Logf("SUCCESS: alice was allowed via LC_SSH_SERVER (host)")
}

// TestACLEnvRoutingDeniedHost checks the same enforcement point for a denied
// user: bob has no ACL rule, so the LC_SSH_SERVER request must be refused.
func TestACLEnvRoutingDeniedHost(t *testing.T) {
	if err := waitForPortHost(aclEnvProxyAddr, 60*time.Second); err != nil {
		t.Fatalf("ACL proxy not ready: %v", err)
	}

	client := dialACLProxy(t, aclEnvProxyAddr, "bob", "user3_key")
	defer client.Close()
	serveACLAgent(t, client, "user3_key")

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	if err := session.Setenv("LC_SSH_SERVER", "testuser@target-host:22"); err == nil {
		t.Fatal("expected LC_SSH_SERVER to be denied by access_control for bob, but it was accepted")
	}
	t.Logf("SUCCESS: bob was denied via LC_SSH_SERVER (host)")
}

func dialACLProxy(t *testing.T, addr, user, keyFile string) *ssh.Client {
	t.Helper()

	keyPath := filepath.Join(aclKeysDir, "clients", keyFile)
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		t.Fatalf("Client key not found at %s", keyPath)
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(loadKeyPairHost(t, keyPath, "")...),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}

	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		t.Fatalf("Failed to connect to proxy at %s as %s: %v", addr, user, err)
	}
	return client
}

// serveACLAgent opens an auth-agent channel and serves the given client key
// on it, so the proxy can use it to authenticate to the target host.
func serveACLAgent(t *testing.T, client *ssh.Client, keyFile string) {
	t.Helper()

	agentChannel, _, err := client.OpenChannel("auth-agent@openssh.com", nil)
	if err != nil {
		t.Fatalf("Failed to open agent channel: %v", err)
	}

	keyPath := filepath.Join(aclKeysDir, "clients", keyFile)
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("Failed to read key: %v", err)
	}
	rawKey, err := ssh.ParseRawPrivateKey(keyData)
	if err != nil {
		t.Fatalf("Failed to parse raw private key: %v", err)
	}

	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: rawKey}); err != nil {
		t.Fatalf("Failed to add key to keyring: %v", err)
	}
	go agent.ServeAgent(keyring, agentChannel)
}

func runACLCommand(t *testing.T, client *ssh.Client, command string) string {
	t.Helper()

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	if err := session.Run(command); err != nil {
		t.Fatalf("Failed to run command %q: %v (stderr: %s)", command, err, stderr.String())
	}
	return stdout.String()
}
