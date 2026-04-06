package server

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestSplitTargetAddress(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantUser string
		wantHost string
		wantPort string
		wantErr  bool
	}{
		{
			name:     "host with explicit port",
			input:    "alice@example.com:2222",
			wantUser: "alice",
			wantHost: "example.com",
			wantPort: "2222",
		},
		{
			name:     "host with default port",
			input:    "alice@example.com",
			wantUser: "alice",
			wantHost: "example.com",
			wantPort: "22",
		},
		{
			name:     "ipv6 host",
			input:    "alice@[2001:db8::1]:2200",
			wantUser: "alice",
			wantHost: "2001:db8::1",
			wantPort: "2200",
		},
		{
			name:     "host without explicit user",
			input:    "example.com:22",
			wantUser: "",
			wantHost: "example.com",
			wantPort: "22",
		},
		{
			name:    "rejects suspicious shell metacharacters",
			input:   "alice@example.com;uname -a",
			wantErr: true,
		},
		{
			name:    "rejects whitespace injection payload",
			input:   "alice@example.com -oProxyCommand=evil",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotUser, gotHost, gotPort, err := splitTargetAddress(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("splitTargetAddress(%q) expected error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("splitTargetAddress(%q) returned error: %v", tt.input, err)
			}
			if gotUser != tt.wantUser || gotHost != tt.wantHost || gotPort != tt.wantPort {
				t.Fatalf("splitTargetAddress(%q) = (%q, %q, %q), want (%q, %q, %q)", tt.input, gotUser, gotHost, gotPort, tt.wantUser, tt.wantHost, tt.wantPort)
			}
		})
	}
}

func TestFormatTargetAddress(t *testing.T) {
	if got := formatTargetAddress("alice", "example.com", "2222"); got != "alice@example.com:2222" {
		t.Fatalf("formatTargetAddress() = %q, want %q", got, "alice@example.com:2222")
	}

	if got := formatTargetAddress("alice", "2001:db8::1", "2222"); got != "alice@[2001:db8::1]:2222" {
		t.Fatalf("formatTargetAddress() with IPv6 = %q, want %q", got, "alice@[2001:db8::1]:2222")
	}

	if got := formatTargetAddress("", "example.com", "22"); got != "" {
		t.Fatalf("formatTargetAddress() with missing user = %q, want empty string", got)
	}
}

func TestParsePtyRequest(t *testing.T) {
	term := "xterm-256color"
	payload := make([]byte, 4+len(term)+8)
	binary.BigEndian.PutUint32(payload[:4], uint32(len(term)))
	copy(payload[4:], term)
	binary.BigEndian.PutUint32(payload[4+len(term):8+len(term)], 120)
	binary.BigEndian.PutUint32(payload[8+len(term):12+len(term)], 40)

	gotTerm, gotCols, gotRows, err := parsePtyRequest(payload)
	if err != nil {
		t.Fatalf("parsePtyRequest() returned error: %v", err)
	}
	if gotTerm != term || gotCols != 120 || gotRows != 40 {
		t.Fatalf("parsePtyRequest() = (%q, %d, %d), want (%q, %d, %d)", gotTerm, gotCols, gotRows, term, 120, 40)
	}
}

func TestParsePtyRequestInvalidPayload(t *testing.T) {
	if _, _, _, err := parsePtyRequest([]byte{1, 2, 3}); err == nil {
		t.Fatal("parsePtyRequest() expected error for short payload")
	}
}

func TestParseTargetFromCommand(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "user host flags", input: "ssh -l alice example.com", want: "alice@example.com"},
		{name: "inline target", input: "ssh bob@example.com", want: "bob@example.com"},
		{name: "no target", input: "echo hello", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseTargetFromCommand(tt.input); got != tt.want {
				t.Fatalf("parseTargetFromCommand(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildRecordingFileName(t *testing.T) {
	got := buildRecordingFileName("alice", "host:name", "22", "session-id", "asciinema")
	if got != "alice_host_name_22_session-id.cast" {
		t.Fatalf("buildRecordingFileName() = %q, want %q", got, "alice_host_name_22_session-id.cast")
	}

	if !strings.HasSuffix(got, ".cast") {
		t.Fatalf("buildRecordingFileName() should end with .cast, got %q", got)
	}

	scriptGot := buildRecordingFileName("alice", "host:name", "22", "session-id", "script")
	if !strings.HasSuffix(scriptGot, ".log") {
		t.Fatalf("buildRecordingFileName() with script format should end with .log, got %q", scriptGot)
	}
}
