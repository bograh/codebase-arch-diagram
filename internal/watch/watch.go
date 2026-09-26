// Package watch reports, debounced, when Go sources under a directory change.
package watch

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher watches a directory tree for changes to Go sources.
type Watcher struct {
	fsw      *fsnotify.Watcher
	debounce time.Duration
	events   chan struct{}
	done     chan struct{}
}

// New watches root recursively. See Events for delivery semantics.
func New(root string, debounce time.Duration) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create watcher: %w", err)
	}
	w := &Watcher{fsw: fsw, debounce: debounce, events: make(chan struct{}, 1), done: make(chan struct{})}
	if err := w.addTree(root); err != nil {
		fsw.Close()
		return nil, err
	}
	go w.loop()
	return w, nil
}

// Events fires once per burst of changes, debounce after the last one. It
// buffers one signal: changes during a slow consumer collapse into one.
func (w *Watcher) Events() <-chan struct{} { return w.events }

// Close stops watching and waits for the watcher goroutine to exit.
func (w *Watcher) Close() error {
	err := w.fsw.Close()
	<-w.done
	return err
}

func (w *Watcher) addTree(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil // a subdirectory vanished or is unreadable; skip it
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && skipDir(d.Name()) {
			return filepath.SkipDir
		}
		if err := w.fsw.Add(path); err != nil {
			return fmt.Errorf("watch %s: %w", path, err)
		}
		return nil
	})
}

func (w *Watcher) loop() {
	defer close(w.done)
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	for {
		select {
		case ev, ok := <-w.fsw.Events:
			if !ok {
				timer.Stop()
				return
			}
			if ev.Has(fsnotify.Create) {
				if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
					if !skipDir(fi.Name()) {
						_ = w.addTree(ev.Name) // best effort: it may already be gone
						timer.Reset(w.debounce)
					}
					continue
				}
			}
			if relevant(ev) {
				timer.Reset(w.debounce)
			}
		case _, ok := <-w.fsw.Errors:
			if !ok {
				timer.Stop()
				return
			}
		case <-timer.C:
			select {
			case w.events <- struct{}{}:
			default: // a signal is already pending
			}
		}
	}
}

func skipDir(name string) bool {
	switch name {
	case "vendor", "testdata", "node_modules":
		return true
	}
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

func relevant(ev fsnotify.Event) bool {
	if ev.Op == fsnotify.Chmod {
		return false
	}
	base := filepath.Base(ev.Name)
	if strings.HasSuffix(base, ".go") || base == "go.mod" || base == "go.sum" {
		return true
	}
	// A removed or renamed directory takes its packages with it.
	return (ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename)) && filepath.Ext(base) == ""
}
