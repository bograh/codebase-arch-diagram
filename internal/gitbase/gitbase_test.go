package gitbase

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"codebase/arch/internal/extract"
	"codebase/arch/internal/model"
)

var ctx = context.Background()

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=test", "-c", "user.email=test@example.com",
		"-c", "commit.gpgsign=false"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newRepo creates a repository whose main branch holds module example.com/r
// with one package, a.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/r\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "a", "a.go"), "package a\n\nfunc A() {}\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func commitPackageB(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "b", "b.go"), "package b\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "add b")
}

type countingExtractor struct{ calls int }

func (c *countingExtractor) Extract(ctx context.Context, dir string) (*model.Graph, error) {
	c.calls++
	return extract.GoExtractor{}.Extract(ctx, dir)
}

func TestGraphIgnoresUncommittedChanges(t *testing.T) {
	dir := newRepo(t)
	writeFile(t, filepath.Join(dir, "b", "b.go"), "package b\n")

	b := &Builder{Extractor: extract.GoExtractor{}, CacheDir: t.TempDir()}
	g, sha, err := b.Graph(ctx, dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if want := gitRun(t, dir, "rev-parse", "HEAD"); sha != want {
		t.Errorf("sha = %s, want %s", sha, want)
	}
	if g.Nodes["example.com/r/a"] == nil || g.Nodes["example.com/r/b"] != nil {
		t.Errorf("nodes = %v, want only the committed package a", g.SortedIDs())
	}
}

func TestGraphCachesBySHA(t *testing.T) {
	dir, cache := newRepo(t), t.TempDir()

	first := &countingExtractor{}
	if _, _, err := (&Builder{Extractor: first, CacheDir: cache}).Graph(ctx, dir, "main"); err != nil {
		t.Fatal(err)
	}
	if first.calls != 1 {
		t.Fatalf("first build extracted %d times, want 1", first.calls)
	}

	second := &countingExtractor{}
	b := &Builder{Extractor: second, CacheDir: cache}
	for range 2 {
		g, _, err := b.Graph(ctx, dir, "main")
		if err != nil {
			t.Fatal(err)
		}
		if g.Nodes["example.com/r/a"] == nil {
			t.Fatal("cached graph lost package a")
		}
	}
	if second.calls != 0 {
		t.Errorf("cached build extracted %d times, want 0", second.calls)
	}
}

func TestGraphRebuildsWhenRefMoves(t *testing.T) {
	dir := newRepo(t)
	ce := &countingExtractor{}
	b := &Builder{Extractor: ce, CacheDir: t.TempDir()}

	_, sha1, err := b.Graph(ctx, dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	commitPackageB(t, dir)
	g, sha2, err := b.Graph(ctx, dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if sha1 == sha2 {
		t.Error("sha did not change after a new commit")
	}
	if g.Nodes["example.com/r/b"] == nil {
		t.Errorf("nodes = %v, want package b", g.SortedIDs())
	}
	if ce.calls != 2 {
		t.Errorf("extracted %d times, want 2", ce.calls)
	}
}

func TestCorruptCacheIsRebuilt(t *testing.T) {
	dir, cache := newRepo(t), t.TempDir()
	if _, _, err := (&Builder{Extractor: extract.GoExtractor{}, CacheDir: cache}).Graph(ctx, dir, "main"); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(cache, "*", "*.json"))
	if len(files) != 1 {
		t.Fatalf("cache files = %v, want exactly one", files)
	}
	if err := os.WriteFile(files[0], []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	ce := &countingExtractor{}
	g, _, err := (&Builder{Extractor: ce, CacheDir: cache}).Graph(ctx, dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if ce.calls != 1 || g.Nodes["example.com/r/a"] == nil {
		t.Errorf("calls = %d, nodes = %v; want a rebuild with package a", ce.calls, g.SortedIDs())
	}
}

func TestWorktreeIsRemoved(t *testing.T) {
	dir := newRepo(t)
	if _, _, err := (&Builder{Extractor: extract.GoExtractor{}, CacheDir: t.TempDir()}).Graph(ctx, dir, "main"); err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(gitRun(t, dir, "worktree", "list"), "\n"); len(lines) != 1 {
		t.Errorf("worktrees left behind:\n%s", strings.Join(lines, "\n"))
	}
}

func TestResolveRefErrors(t *testing.T) {
	if _, err := ResolveRef(ctx, newRepo(t), "no-such-ref"); err == nil {
		t.Error("unknown ref resolved, want error")
	}
	if _, err := ResolveRef(ctx, t.TempDir(), "main"); err == nil {
		t.Error("ref resolved outside a repository, want error")
	}
}
