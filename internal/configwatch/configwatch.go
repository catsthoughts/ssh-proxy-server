// Package configwatch watches configuration-related files with fsnotify and
// debounces change events. It supports the hot-reload requirement (FR-3):
// changes to the config file or authorized_keys are picked up without
// restarting the proxy and without breaking active sessions.
package configwatch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watch watches the given file paths and invokes onEvent (serialized, after a
// short debounce) whenever any of them is created, written, or renamed onto.
// Missing files are tolerated: their parent directory is watched instead so a
// later create/rename is still observed. Call Close to release the watcher.
func Watch(ctx context.Context, paths []string, onEvent func(changedPath string)) (*Watcher, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("configwatch: no paths to watch")
	}
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("configwatch: failed to create fsnotify watcher: %w", err)
	}

	w := &Watcher{
		watcher: fsWatcher,
		onEvent: onEvent,
		pending: make(map[string]struct{}),
		done:    make(chan struct{}),
	}

	for _, path := range paths {
		if path == "" {
			continue
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			w.Close()
			return nil, fmt.Errorf("configwatch: failed to resolve %s: %w", path, err)
		}
		if _, err := os.Stat(abs); err == nil {
			if err := fsWatcher.Add(abs); err != nil {
				w.Close()
				return nil, fmt.Errorf("configwatch: failed to watch %s: %w", path, err)
			}
			continue
		}
		// The file does not exist yet; watch the parent directory instead.
		if err := fsWatcher.Add(filepath.Dir(abs)); err != nil {
			w.Close()
			return nil, fmt.Errorf("configwatch: failed to watch directory of %s: %w", path, err)
		}
	}

	go w.loop(ctx)
	return w, nil
}

// Watcher wraps an fsnotify watcher with debounce handling.
type Watcher struct {
	watcher *fsnotify.Watcher
	onEvent func(string)

	mu      sync.Mutex
	pending map[string]struct{}
	timer   *time.Timer
	done    chan struct{}
	closed  bool
}

const debounce = 300 * time.Millisecond

func (w *Watcher) loop(ctx context.Context) {
	defer close(w.done)
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}
			w.schedule(event.Name)
		case _, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
		}
	}
}

func (w *Watcher) schedule(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.pending[path] = struct{}{}
	if w.timer != nil {
		w.timer.Stop()
	}
	w.timer = time.AfterFunc(debounce, w.flush)
}

func (w *Watcher) flush() {
	w.mu.Lock()
	paths := make([]string, 0, len(w.pending))
	for path := range w.pending {
		paths = append(paths, path)
	}
	w.pending = make(map[string]struct{})
	w.mu.Unlock()

	for _, path := range paths {
		if w.onEvent != nil {
			w.onEvent(path)
		}
	}
}

// Close stops the watcher and waits for the event loop to finish.
func (w *Watcher) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		<-w.done
		return nil
	}
	w.closed = true
	if w.timer != nil {
		w.timer.Stop()
	}
	w.mu.Unlock()

	err := w.watcher.Close()
	<-w.done
	return err
}
