package server

import (
	"bytes"
	"encoding/binary"
	"io"
	"log"
	"reflect"
	"strings"
	"testing"

	"ssh-proxy-server/internal/types"

	"golang.org/x/crypto/ssh"
)

func TestSanitizeFileComponent(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "keeps safe characters", input: "alpha-_.123", want: "alpha-_.123"},
		{name: "replaces unsafe characters", input: " host:name/42 ", want: "host_name_42"},
		{name: "empty after trimming", input: " ._ ", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeFileComponent(tt.input); got != tt.want {
				t.Fatalf("sanitizeFileComponent(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseWindowChange(t *testing.T) {
	payload := make([]byte, 8)
	binary.BigEndian.PutUint32(payload[:4], 132)
	binary.BigEndian.PutUint32(payload[4:8], 43)

	cols, rows := 0, 0
	parseWindowChange(payload, &cols, &rows)

	if cols != 132 || rows != 43 {
		t.Fatalf("parseWindowChange() = (%d, %d), want (%d, %d)", cols, rows, 132, 43)
	}
}

func TestParseWindowChangeShortPayloadKeepsValues(t *testing.T) {
	cols, rows := 80, 24
	parseWindowChange([]byte{1, 2, 3}, &cols, &rows)

	if cols != 80 || rows != 24 {
		t.Fatalf("short payload should keep values unchanged, got (%d, %d)", cols, rows)
	}
}

func TestHandleEnvRequestStoresTargetDetails(t *testing.T) {
	state := &types.SessionState{ClientUser: "alice", EnvVars: make(map[string]string)}
	req := &ssh.Request{
		Type:      "env",
		WantReply: false,
		Payload:   encodeEnvPayload("LC_SSH_SERVER", "example.com:2222"),
	}

	handleEnvRequest(req, state)

	if got := state.EnvVars["LC_SSH_SERVER"]; got != "example.com:2222" {
		t.Fatalf("EnvVars[LC_SSH_SERVER] = %q, want %q", got, "example.com:2222")
	}
	if state.TargetUser != "alice" || state.TargetHost != "example.com" || state.TargetPort != "2222" {
		t.Fatalf("parsed target = (%q, %q, %q), want (%q, %q, %q)", state.TargetUser, state.TargetHost, state.TargetPort, "alice", "example.com", "2222")
	}
}

func TestHandleEnvRequestInvalidPayloadDoesNotMutateState(t *testing.T) {
	state := &types.SessionState{EnvVars: map[string]string{"KEEP": "value"}}
	req := &ssh.Request{Type: "env", WantReply: false, Payload: []byte{1, 2, 3}}

	handleEnvRequest(req, state)

	if len(state.EnvVars) != 1 || state.EnvVars["KEEP"] != "value" {
		t.Fatalf("expected existing env vars to remain unchanged, got %#v", state.EnvVars)
	}
	if state.TargetUser != "" || state.TargetHost != "" || state.TargetPort != "" {
		t.Fatalf("expected target fields to remain empty, got (%q, %q, %q)", state.TargetUser, state.TargetHost, state.TargetPort)
	}
}

func TestHandleEnvRequestRejectsSuspiciousTargetAndLogs(t *testing.T) {
	types.SetLogLevel("info")

	var logBuffer bytes.Buffer
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&logBuffer)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	}()

	state := &types.SessionState{ClientUser: "alice", EnvVars: map[string]string{"KEEP": "value"}}
	req := &ssh.Request{
		Type:      "env",
		WantReply: false,
		Payload:   encodeEnvPayload("LC_SSH_SERVER", "alice@example.com;uname -a"),
	}

	handleEnvRequest(req, state)

	if _, exists := state.EnvVars["LC_SSH_SERVER"]; exists {
		t.Fatalf("expected suspicious LC_SSH_SERVER to be rejected, got %#v", state.EnvVars)
	}
	if state.TargetUser != "" || state.TargetHost != "" || state.TargetPort != "" {
		t.Fatalf("expected target fields to remain empty, got (%q, %q, %q)", state.TargetUser, state.TargetHost, state.TargetPort)
	}
	if got := logBuffer.String(); !strings.Contains(got, "Rejected suspicious LC_SSH_SERVER value") {
		t.Fatalf("expected suspicious input to be logged, got %q", got)
	}
}

func TestHandleExecProxyRejectsDirectCommands(t *testing.T) {
	channel := &stubSSHChannel{}
	state := &types.SessionState{EnvVars: make(map[string]string)}

	handleExecProxy(channel, state, "uname -a")

	if got := channel.stdout.String(); !strings.Contains(got, "direct commands are disabled") {
		t.Fatalf("handleExecProxy() output = %q, want direct command rejection message", got)
	}
	if len(channel.requests) != 1 || channel.requests[0].name != "exit-status" {
		t.Fatalf("expected one exit-status request, got %#v", channel.requests)
	}
	if !channel.closed || !channel.closeWriteCalled {
		t.Fatalf("expected channel to be closed after rejecting exec request")
	}
}

func TestResolveTargetCandidatesUsesStaticRoutingAndIgnoresEnv(t *testing.T) {
	state := &types.SessionState{
		StaticRoutingEnabled: true,
		StaticTargets:        []string{"primary.example.com:22", "backup.example.com:22"},
		StaticRoutingMode:    RoutingModeFailover,
		EnvVars:              map[string]string{"LC_SSH_SERVER": "ignored.example.net:2222"},
	}

	got, err := resolveTargetCandidates(state, "ignored.example.net:2222", "ssh other.example.net")
	if err != nil {
		t.Fatalf("resolveTargetCandidates() returned error: %v", err)
	}
	if !reflect.DeepEqual(got, state.StaticTargets) {
		t.Fatalf("resolveTargetCandidates() = %#v, want %#v", got, state.StaticTargets)
	}
}

func TestResolveTargetAddressUsesSessionUserWhenMissingInRoute(t *testing.T) {
	user, host, port, err := resolveTargetAddress("target.example.com:2200", "alice")
	if err != nil {
		t.Fatalf("resolveTargetAddress() returned error: %v", err)
	}
	if user != "alice" || host != "target.example.com" || port != "2200" {
		t.Fatalf("resolveTargetAddress() = (%q, %q, %q), want (%q, %q, %q)", user, host, port, "alice", "target.example.com", "2200")
	}
}

func TestOrderedStaticTargetsRoundRobinRotates(t *testing.T) {
	staticRouteCounter = 0
	targets := []string{"one.example.com:22", "two.example.com:22", "three.example.com:22"}

	first := orderedStaticTargets(targets, RoutingModeRoundRobin)
	second := orderedStaticTargets(targets, RoutingModeRoundRobin)

	if !reflect.DeepEqual(first, []string{"one.example.com:22", "two.example.com:22", "three.example.com:22"}) {
		t.Fatalf("first orderedStaticTargets() = %#v", first)
	}
	if !reflect.DeepEqual(second, []string{"two.example.com:22", "three.example.com:22", "one.example.com:22"}) {
		t.Fatalf("second orderedStaticTargets() = %#v", second)
	}
}

type stubSSHChannel struct {
	stdout           bytes.Buffer
	stderr           bytes.Buffer
	requests         []stubChannelRequest
	closed           bool
	closeWriteCalled bool
}

type stubChannelRequest struct {
	name    string
	payload []byte
}

func (c *stubSSHChannel) Read(p []byte) (int, error) {
	return 0, io.EOF
}

func (c *stubSSHChannel) Write(p []byte) (int, error) {
	return c.stdout.Write(p)
}

func (c *stubSSHChannel) Close() error {
	c.closed = true
	return nil
}

func (c *stubSSHChannel) CloseWrite() error {
	c.closeWriteCalled = true
	return nil
}

func (c *stubSSHChannel) SendRequest(name string, wantReply bool, payload []byte) (bool, error) {
	c.requests = append(c.requests, stubChannelRequest{name: name, payload: payload})
	return false, nil
}

func (c *stubSSHChannel) Stderr() io.ReadWriter {
	return &c.stderr
}

func encodeEnvPayload(name, value string) []byte {
	payload := make([]byte, 4+len(name)+4+len(value))
	binary.BigEndian.PutUint32(payload[:4], uint32(len(name)))
	copy(payload[4:], name)
	offset := 4 + len(name)
	binary.BigEndian.PutUint32(payload[offset:offset+4], uint32(len(value)))
	copy(payload[offset+4:], value)
	return payload
}
