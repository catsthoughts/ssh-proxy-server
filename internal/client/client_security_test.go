package client

import (
	"path/filepath"
	"strings"
	"testing"

	"ssh-proxy-server/internal/types"
)

func TestGetHostKeyCallbackRequiresKnownHostsByDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SSH_PROXY_INSECURE_IGNORE_HOSTKEY", "")

	if _, err := getHostKeyCallback(nil); err == nil {
		t.Fatal("expected error when known_hosts is missing")
	}
}

func TestGetHostKeyCallbackAllowsExplicitInsecureFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SSH_PROXY_INSECURE_IGNORE_HOSTKEY", "1")

	if _, err := getHostKeyCallback(nil); err != nil {
		t.Fatalf("getHostKeyCallback() returned error with explicit insecure fallback: %v", err)
	}
}

func TestGetHostKeyCallbackAllowsExplicitInsecureFlag(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SSH_PROXY_INSECURE_IGNORE_HOSTKEY", "")

	state := &types.SessionState{InsecureIgnoreHostKey: true}
	if _, err := getHostKeyCallback(state); err != nil {
		t.Fatalf("getHostKeyCallback() returned error with explicit startup flag: %v", err)
	}
}

func TestGetSSHAgentConnRejectsLocalFallbackByDefault(t *testing.T) {
	t.Setenv("SSH_PROXY_ALLOW_LOCAL_AGENT_FALLBACK", "")
	t.Setenv("SSH_AUTH_SOCK", filepath.Join(t.TempDir(), "agent.sock"))

	_, err := GetSSHAgentConn(nil)
	if err == nil {
		t.Fatal("expected GetSSHAgentConn() to reject local agent fallback by default")
	}
	if !strings.Contains(err.Error(), "ssh -A") && !strings.Contains(strings.ToLower(err.Error()), "forward") {
		t.Fatalf("expected forwarded-agent error, got %q", err.Error())
	}
}
