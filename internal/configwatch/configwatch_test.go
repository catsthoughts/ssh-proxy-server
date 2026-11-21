package configwatch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func waitForEvent(t *testing.T, events <-chan string, timeout time.Duration) string {
	t.Helper()
	select {
	case path := <-events:
		return path
	case <-time.After(timeout):
		t.Fatal("timed out waiting for watch event")
		return ""
	}
}

func assertNoEvent(t *testing.T, events <-chan string, timeout time.Duration) {
	t.Helper()
	select {
	case path := <-events:
		t.Fatalf("unexpected watch event for %q", path)
	case <-time.After(timeout):
	}
}

func newWatcher(t *testing.T, paths ...string) (*Watcher, chan string) {
	t.Helper()
	events := make(chan string, 8)
	w, err := Watch(context.Background(), paths, func(changed string) {
		events <- changed
	})
	if err != nil {
		t.Fatalf("Watch() returned error: %v", err)
	}
	t.Cleanup(func() { w.Close() })
	return w, events
}

func TestWatchDetectsWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, events := newWatcher(t, path)

	if err := os.WriteFile(path, []byte(`{"log_level":"debug"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := waitForEvent(t, events, 5*time.Second); got != path {
		t.Fatalf("watch event path = %q, want %q", got, path)
	}
}

// Regression (FR-3.1): editors like vim save atomically by writing a new
// file and renaming it over the old one. The watcher must observe the
// replacement instead of losing the watch on the old inode.
func TestWatchDetectsAtomicRenameReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, events := newWatcher(t, path)

	tmpPath := filepath.Join(dir, "config.json.new")
	if err := os.WriteFile(tmpPath, []byte(`{"log_level":"debug"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		t.Fatal(err)
	}

	if got := waitForEvent(t, events, 5*time.Second); got != path {
		t.Fatalf("watch event path = %q, want %q", got, path)
	}

	// A second atomic save must still be observed: the watch must survive
	// the first rename.
	if err := os.WriteFile(tmpPath, []byte(`{"log_level":"info"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		t.Fatal(err)
	}

	if got := waitForEvent(t, events, 5*time.Second); got != path {
		t.Fatalf("second watch event path = %q, want %q", got, path)
	}
}

func TestWatchDetectsFileCreatedLater(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "authorized_keys")

	_, events := newWatcher(t, path)

	if err := os.WriteFile(path, []byte("ssh-ed25519 AAAA..."), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := waitForEvent(t, events, 5*time.Second); got != path {
		t.Fatalf("watch event path = %q, want %q", got, path)
	}
}

func TestWatchIgnoresUnrelatedFilesInSameDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, events := newWatcher(t, path)

	other := filepath.Join(dir, "unrelated.txt")
	if err := os.WriteFile(other, []byte("noise"), 0o600); err != nil {
		t.Fatal(err)
	}

	assertNoEvent(t, events, time.Second)
}

func TestWatchRequiresPaths(t *testing.T) {
	if _, err := Watch(context.Background(), nil, func(string) {}); err == nil {
		t.Fatal("expected an error when no paths are provided")
	}
}

func TestWatchMultiplePaths(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	keysPath := filepath.Join(dir, "authorized_keys")
	if err := os.WriteFile(configPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keysPath, []byte("k"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, events := newWatcher(t, configPath, keysPath)

	if err := os.WriteFile(keysPath, []byte("k2"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := waitForEvent(t, events, 5*time.Second); got != keysPath {
		t.Fatalf("watch event path = %q, want %q", got, keysPath)
	}
}
