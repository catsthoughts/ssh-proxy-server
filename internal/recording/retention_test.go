package recording

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteChecksum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alice_web1_22_rec.cast")
	content := []byte(`{"version":2}` + "\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := WriteChecksum(path); err != nil {
		t.Fatalf("WriteChecksum() error: %v", err)
	}

	sum := sha256.Sum256(content)
	want := hex.EncodeToString(sum[:])

	checksumData, err := os.ReadFile(path + ChecksumExtension)
	if err != nil {
		t.Fatalf("checksum sidecar missing: %v", err)
	}
	fields := strings.Fields(string(checksumData))
	if len(fields) != 2 || fields[0] != want || fields[1] != filepath.Base(path) {
		t.Fatalf("checksum content = %q, want %q + filename", checksumData, want)
	}
}

func TestWriteChecksumMissingFile(t *testing.T) {
	if err := WriteChecksum(filepath.Join(t.TempDir(), "missing.cast")); err == nil {
		t.Fatal("WriteChecksum() expected error for missing recording")
	}
}

func TestEnforceRetention(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	old := filepath.Join(dir, "old_user_host_22_rec.cast")
	if err := os.WriteFile(old, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldTime := now.Add(-10 * 24 * time.Hour)
	if err := os.Chtimes(old, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	oldChecksum := old + ChecksumExtension
	if err := os.WriteFile(oldChecksum, []byte("sum"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(oldChecksum, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	fresh := filepath.Join(dir, "fresh_user_host_22_rec.cast")
	if err := os.WriteFile(fresh, []byte("fresh"), 0o600); err != nil {
		t.Fatal(err)
	}

	other := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(other, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(other, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	removed, err := EnforceRetention(dir, 7, now)
	if err != nil {
		t.Fatalf("EnforceRetention() error: %v", err)
	}
	if len(removed) != 2 {
		t.Fatalf("EnforceRetention() removed %d files, want 2", len(removed))
	}

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("expired recording should have been removed")
	}
	if _, err := os.Stat(oldChecksum); !os.IsNotExist(err) {
		t.Fatal("expired recording checksum should have been removed")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("fresh recording should be kept")
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("unrelated files should be kept")
	}
}

func TestEnforceRetentionDisabled(t *testing.T) {
	removed, err := EnforceRetention(t.TempDir(), 0, time.Now())
	if err != nil || len(removed) != 0 {
		t.Fatalf("EnforceRetention() = %v, %v; want no-op", removed, err)
	}
}

func TestEnforceRetentionMissingDir(t *testing.T) {
	if _, err := EnforceRetention(filepath.Join(t.TempDir(), "missing"), 7, time.Now()); err != nil {
		t.Fatalf("EnforceRetention() error = %v, want nil for missing dir", err)
	}
}
