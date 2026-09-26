package watch

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const debounce = 50 * time.Millisecond

func newWatcher(t *testing.T, root string) *Watcher {
	t.Helper()
	w, err := New(root, debounce)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	return w
}

func expectEvent(t *testing.T, w *Watcher) {
	t.Helper()
	select {
	case <-w.Events():
	case <-time.After(2 * time.Second):
		t.Fatal("expected a change event")
	}
}

func expectQuiet(t *testing.T, w *Watcher) {
	t.Helper()
	select {
	case <-w.Events():
		t.Fatal("unexpected change event")
	case <-time.After(300 * time.Millisecond):
	}
}

func write(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBurstIsDebouncedIntoOneEvent(t *testing.T) {
	dir := t.TempDir()
	w := newWatcher(t, dir)
	for i := range 5 {
		write(t, filepath.Join(dir, fmt.Sprintf("f%d.go", i)))
	}
	expectEvent(t, w)
	expectQuiet(t, w)
}

func TestIgnoresNonGoFiles(t *testing.T) {
	dir := t.TempDir()
	w := newWatcher(t, dir)
	write(t, filepath.Join(dir, "notes.txt"))
	expectQuiet(t, w)
}

func TestWatchesNewDirectories(t *testing.T) {
	dir := t.TempDir()
	w := newWatcher(t, dir)
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	expectEvent(t, w)
	write(t, filepath.Join(sub, "x.go"))
	expectEvent(t, w)
}

func TestSkipsVendorAndHiddenDirectories(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"vendor", ".git", "testdata"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	w := newWatcher(t, dir)
	for _, d := range []string{"vendor", ".git", "testdata"} {
		write(t, filepath.Join(dir, d, "x.go"))
	}
	expectQuiet(t, w)
}
