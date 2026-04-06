package client

import (
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

func TestGetHostKeyCallbackAllowsExplicitInsecureOverrideEnv(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SSH_PROXY_INSECURE_IGNORE_HOSTKEY", "1")

	if _, err := getHostKeyCallback(nil); err != nil {
		t.Fatalf("getHostKeyCallback() returned error with explicit insecure override env: %v", err)
	}
}

func TestGetHostKeyCallbackAllowsExplicitInsecureStateFlag(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SSH_PROXY_INSECURE_IGNORE_HOSTKEY", "")

	state := &types.SessionState{InsecureIgnoreHostKey: true}
	if _, err := getHostKeyCallback(state); err != nil {
		t.Fatalf("getHostKeyCallback() returned error with explicit startup flag: %v", err)
	}
}

func TestGetSSHAgentConnRequiresForwardedAgent(t *testing.T) {
	_, err := GetSSHAgentConn(nil)
	if err == nil {
		t.Fatal("expected GetSSHAgentConn() to require forwarded SSH agent access")
	}
	if !strings.Contains(err.Error(), "ssh -A") && !strings.Contains(strings.ToLower(err.Error()), "forward") {
		t.Fatalf("expected forwarded-agent error, got %q", err.Error())
	}
}
