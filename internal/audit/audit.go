// Package audit implements the structured JSON audit log required for the
// journald -> Loki -> Alertmanager pipeline. Every audit event is emitted as a
// single JSON object on stdout with a stable set of fields ("who / where /
// result / recording_id"), so it can be scraped and alerted on without any
// free-text parsing.
package audit

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Event is a single audit log entry. Fields are omitted when empty so the
// resulting JSON stays compact.
type Event struct {
	Timestamp   time.Time `json:"ts"`
	Event       string    `json:"event"`
	Level       string    `json:"level"`
	SessionID   string    `json:"session_id,omitempty"`
	User        string    `json:"user,omitempty"`
	ClientAddr  string    `json:"client_addr,omitempty"`
	Key         string    `json:"key_fingerprint,omitempty"`
	TargetUser  string    `json:"target_user,omitempty"`
	TargetHost  string    `json:"target_host,omitempty"`
	TargetPort  string    `json:"target_port,omitempty"`
	SSOResult   string    `json:"sso_result,omitempty"`
	RecordingID string    `json:"recording_id,omitempty"`
	Result      string    `json:"result,omitempty"`
	ExitStatus  *int      `json:"exit_status,omitempty"`
	DurationSec float64   `json:"duration_s,omitempty"`
	Reason      string    `json:"reason,omitempty"`
}

// DeriveLevel maps an event result to a severity level so Loki/Alertmanager
// can filter on it without free-text parsing.
func DeriveLevel(result string) string {
	switch strings.ToLower(strings.TrimSpace(result)) {
	case "denied", "failure":
		return "warning"
	default:
		return "info"
	}
}

// Int returns a pointer to an int, for setting optional numeric fields such
// as ExitStatus (where 0 is a meaningful value).
func Int(value int) *int {
	return &value
}

// Logger writes audit events as JSON lines.
type Logger struct {
	mu sync.Mutex
	w  io.Writer
}

// New returns a Logger writing to w. If w is nil, os.Stdout is used.
func New(w io.Writer) *Logger {
	if w == nil {
		w = os.Stdout
	}
	return &Logger{w: w}
}

// Log emits a single audit event as one JSON line.
func (l *Logger) Log(e Event) {
	if l == nil {
		return
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	if e.Level == "" {
		e.Level = DeriveLevel(e.Result)
	}
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.w.Write(append(line, '\n'))
}
