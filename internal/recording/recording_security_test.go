package recording

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewAsciinemaRecorderUsesPrivateFilePermissions(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "session.cast")
	recorder := NewAsciinemaRecorder(filePath)
	defer recorder.Close()

	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("Stat() returned error: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("recording file permissions = %o, want %o", mode, 0o600)
	}
}
