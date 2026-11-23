package audit

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestLoggerWritesJSONLine(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf)

	logger.Log(Event{
		Timestamp:   time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC),
		Event:       "session_end",
		SessionID:   "sess-1",
		User:        "alice",
		TargetHost:  "web1.example.com",
		TargetPort:  "22",
		SSOResult:   "success",
		RecordingID: "rec-123",
		Result:      "success",
		ExitStatus:  Int(0),
		DurationSec: 42.5,
	})

	line := strings.TrimSpace(buf.String())
	if !strings.HasPrefix(line, "{") || !strings.HasSuffix(line, "}") {
		t.Fatalf("audit output is not a JSON object: %q", line)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(line), &decoded); err != nil {
		t.Fatalf("audit output is not valid JSON: %v", err)
	}

	for key, want := range map[string]interface{}{
		"event":        "session_end",
		"level":        "info",
		"session_id":   "sess-1",
		"user":         "alice",
		"target_host":  "web1.example.com",
		"target_port":  "22",
		"sso_result":   "success",
		"recording_id": "rec-123",
		"result":       "success",
	} {
		if decoded[key] != want {
			t.Fatalf("audit field %q = %v, want %v", key, decoded[key], want)
		}
	}
	if decoded["ts"] != "2026-11-21T12:00:00Z" {
		t.Fatalf("audit ts = %v, want 2026-11-21T12:00:00Z", decoded["ts"])
	}
	if decoded["exit_status"] != float64(0) {
		t.Fatalf("audit exit_status = %v, want 0", decoded["exit_status"])
	}
}

func TestLoggerOmitsEmptyFields(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf)
	logger.Log(Event{Event: "auth_success", Result: "success"})

	var decoded map[string]interface{}
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &decoded); err != nil {
		t.Fatalf("audit output is not valid JSON: %v", err)
	}
	for _, omitted := range []string{"recording_id", "target_host", "reason", "client_addr", "exit_status"} {
		if _, ok := decoded[omitted]; ok {
			t.Fatalf("expected field %q to be omitted, got %v", omitted, decoded[omitted])
		}
	}
}

func TestNilLoggerIsSafe(t *testing.T) {
	var logger *Logger
	logger.Log(Event{Event: "noop"}) // must not panic
}

func TestLoggerDerivesLevelFromResult(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf)

	logger.Log(Event{Event: "target_denied", Result: "denied"})
	logger.Log(Event{Event: "sso_failure", Result: "failure"})
	logger.Log(Event{Event: "custom", Result: "success", Level: "debug"})

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 audit lines, got %d", len(lines))
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &decoded); err != nil {
		t.Fatalf("invalid audit line %q: %v", lines[0], err)
	}
	if decoded["level"] != "warning" {
		t.Fatalf("level for denied event = %v, want warning", decoded["level"])
	}
	if err := json.Unmarshal([]byte(lines[1]), &decoded); err != nil {
		t.Fatalf("invalid audit line %q: %v", lines[1], err)
	}
	if decoded["level"] != "warning" {
		t.Fatalf("level for failure event = %v, want warning", decoded["level"])
	}
	if err := json.Unmarshal([]byte(lines[2]), &decoded); err != nil {
		t.Fatalf("invalid audit line %q: %v", lines[2], err)
	}
	if decoded["level"] != "debug" {
		t.Fatalf("explicit level = %v, want debug", decoded["level"])
	}
}

func TestLoggerSerializesConcurrentWrites(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf)
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 50; j++ {
				logger.Log(Event{Event: "concurrent", Result: "success"})
			}
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 400 {
		t.Fatalf("expected 400 audit lines, got %d", len(lines))
	}
	for _, line := range lines {
		var decoded map[string]interface{}
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Fatalf("invalid audit line %q: %v", line, err)
		}
	}
}
