package server

import (
	"encoding/binary"
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
	state := &types.SessionState{EnvVars: make(map[string]string)}
	req := &ssh.Request{
		Type:      "env",
		WantReply: false,
		Payload:   encodeEnvPayload("LC_SSH_SERVER", "alice@example.com:2222"),
	}

	handleEnvRequest(req, state)

	if got := state.EnvVars["LC_SSH_SERVER"]; got != "alice@example.com:2222" {
		t.Fatalf("EnvVars[LC_SSH_SERVER] = %q, want %q", got, "alice@example.com:2222")
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

func encodeEnvPayload(name, value string) []byte {
	payload := make([]byte, 4+len(name)+4+len(value))
	binary.BigEndian.PutUint32(payload[:4], uint32(len(name)))
	copy(payload[4:], name)
	offset := 4 + len(name)
	binary.BigEndian.PutUint32(payload[offset:offset+4], uint32(len(value)))
	copy(payload[offset+4:], value)
	return payload
}
