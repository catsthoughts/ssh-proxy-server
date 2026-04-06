package client

import (
	"net"
	"os"
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

func TestGetSSHAgentConnRequiresForwardedAgentEvenWhenFallbackEnvIsSet(t *testing.T) {
	sockPath := filepath.Join(os.TempDir(), filepath.Base(t.TempDir())+".sock")
	_ = os.Remove(sockPath)
	defer os.Remove(sockPath)

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to create test unix socket: %v", err)
	}
	defer listener.Close()

	accepted := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			close(accepted)
			_ = conn.Close()
		}
	}()

	t.Setenv("SSH_PROXY_ALLOW_LOCAL_AGENT_FALLBACK", "1")
	t.Setenv("SSH_AUTH_SOCK", sockPath)

	_, err = GetSSHAgentConn(nil)
	select {
	case <-accepted:
	default:
	}
	if err == nil {
		t.Fatal("expected GetSSHAgentConn() to reject local SSH agent fallback")
	}
	if !strings.Contains(err.Error(), "ssh -A") && !strings.Contains(strings.ToLower(err.Error()), "forward") {
		t.Fatalf("expected forwarded-agent error, got %q", err.Error())
	}
}
