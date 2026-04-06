package recording

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAsciinemaRecorderWritesHeaderAndFrames(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "session.cast")

	recorder := NewAsciinemaRecorder(filePath)
	defer recorder.Close()

	if err := recorder.Write([]byte("hello")); err != nil {
		t.Fatalf("Write() returned error: %v", err)
	}
	if err := recorder.WriteInput([]byte("ls\n")); err != nil {
		t.Fatalf("WriteInput() returned error: %v", err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile() returned error: %v", err)
	}

	lines := splitNonEmptyLines(string(content))
	if len(lines) != 3 {
		t.Fatalf("expected 3 JSON lines, got %d: %q", len(lines), string(content))
	}

	var header AsciinemaHeader
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatalf("failed to unmarshal header: %v", err)
	}
	if header.Version != 2 {
		t.Fatalf("header version = %d, want 2", header.Version)
	}
	if header.Title != "SSH Proxy Session" {
		t.Fatalf("header title = %q, want %q", header.Title, "SSH Proxy Session")
	}

	assertFrame(t, lines[1], "o", "hello")
	assertFrame(t, lines[2], "i", "ls\n")
}

func TestAsciinemaRecorderDisabledOnCreateError(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "missing", "session.cast")
	recorder := NewAsciinemaRecorder(filePath)
	defer recorder.Close()

	if recorder.enabled {
		t.Fatal("expected recorder to be disabled when file creation fails")
	}
	if err := recorder.Write([]byte("hello")); err != nil {
		t.Fatalf("Write() should be a no-op for disabled recorder, got error: %v", err)
	}
	if err := recorder.WriteInput([]byte("input")); err != nil {
		t.Fatalf("WriteInput() should be a no-op for disabled recorder, got error: %v", err)
	}
}

func TestNewRecorderScriptWritesTranscript(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "session.log")
	recorder, err := NewRecorder("script", filePath)
	if err != nil {
		t.Fatalf("NewRecorder(script) returned error: %v", err)
	}
	defer recorder.Close()

	if err := recorder.WriteInput([]byte("ls\n")); err != nil {
		t.Fatalf("WriteInput() returned error: %v", err)
	}
	if err := recorder.Write([]byte("file1\nfile2\n")); err != nil {
		t.Fatalf("Write() returned error: %v", err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile() returned error: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "Script started on") {
		t.Fatalf("expected script header in transcript, got %q", text)
	}
	if !strings.Contains(text, "ls\n") || !strings.Contains(text, "file1\nfile2\n") {
		t.Fatalf("expected transcript to contain input and output, got %q", text)
	}
}

func TestNewRecorderRejectsUnknownFormat(t *testing.T) {
	if _, err := NewRecorder("unknown", filepath.Join(t.TempDir(), "session.out")); err == nil {
		t.Fatal("expected NewRecorder() to reject an unsupported format")
	}
}

func TestGenerateRecordingID(t *testing.T) {
	id := generateRecordingID()
	if len(id) != 8 {
		t.Fatalf("generateRecordingID() length = %d, want 8", len(id))
	}
}

func assertFrame(t *testing.T, line, wantType, wantData string) {
	t.Helper()

	var frame []interface{}
	if err := json.Unmarshal([]byte(line), &frame); err != nil {
		t.Fatalf("failed to unmarshal frame %q: %v", line, err)
	}
	if len(frame) != 3 {
		t.Fatalf("frame length = %d, want 3", len(frame))
	}
	if got := frame[1]; got != wantType {
		t.Fatalf("frame type = %v, want %q", got, wantType)
	}
	if got := frame[2]; got != wantData {
		t.Fatalf("frame data = %v, want %q", got, wantData)
	}
}

func splitNonEmptyLines(content string) []string {
	var lines []string
	start := 0
	for i, r := range content {
		if r == '\n' {
			if i > start {
				lines = append(lines, content[start:i])
			}
			start = i + 1
		}
	}
	if start < len(content) {
		lines = append(lines, content[start:])
	}
	return lines
}
