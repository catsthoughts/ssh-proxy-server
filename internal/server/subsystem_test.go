package server

import (
	"encoding/binary"
	"testing"
)

func TestParseSubsystemRequest(t *testing.T) {
	payload := make([]byte, 4+4)
	binary.BigEndian.PutUint32(payload[:4], 4)
	copy(payload[4:], "sftp")

	if got := parseSubsystemRequest(payload); got != "sftp" {
		t.Fatalf("parseSubsystemRequest() = %q, want sftp", got)
	}
	if got := parseSubsystemRequest(nil); got != "" {
		t.Fatalf("parseSubsystemRequest(nil) = %q, want empty", got)
	}
	if got := parseSubsystemRequest([]byte{1, 2}); got != "" {
		t.Fatalf("parseSubsystemRequest(short) = %q, want empty", got)
	}
	if got := parseSubsystemRequest([]byte{0, 0, 0, 10, 's', 'f'}); got != "" {
		t.Fatalf("parseSubsystemRequest(truncated) = %q, want empty", got)
	}
}
