// Package gitbase builds the architecture graph of a git ref, caching it by
// commit SHA.
package gitbase

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"codebase/arch/internal/extract"
	"codebase/arch/internal/model"
)

// Builder builds base graphs from git refs. Set Extractor and CacheDir
// before use. It is safe for concurrent use.
type Builder struct {
	Extractor extract.Extractor
	CacheDir  string

	mu      sync.Mutex
	lastSHA string
	last    *model.Graph
}

// DefaultCacheDir is the per-user cache location for base graphs.
func DefaultCacheDir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate cache dir: %w", err)
	}
	return filepath.Join(dir, "archdiag"), nil
}

// ResolveRef returns the commit SHA that ref names in the repository
// containing dir.
func ResolveRef(ctx context.Context, dir, ref string) (string, error) {
	return git(ctx, dir, "rev-parse", "--verify", ref+"^{commit}")
}

// Prune removes worktree records left behind by interrupted runs.
func Prune(ctx context.Context, dir string) error {
	_, err := git(ctx, dir, "worktree", "prune")
	return err
}

// Graph returns the unfiltered graph of the module at dir as of ref, and the
// commit SHA it came from. Callers must not modify the returned graph.
func (b *Builder) Graph(ctx context.Context, dir, ref string) (*model.Graph, string, error) {
	sha, err := ResolveRef(ctx, dir, ref)
	if err != nil {
		return nil, "", err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.last != nil && b.lastSHA == sha {
		return b.last, sha, nil
	}

	root, err := git(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, "", err
	}
	rel, err := relToRoot(root, dir)
	if err != nil {
		return nil, "", err
	}
	path := b.cachePath(root, rel, sha)
	g, err := readCache(path)
	if err != nil { // missing or unreadable: rebuild and overwrite
		if g, err = b.build(ctx, root, rel, sha); err != nil {
			return nil, "", err
		}
		_ = writeCache(path, g) // the cache is an optimisation; a failed write only costs a rebuild
	}
	b.lastSHA, b.last = sha, g
	return g, sha, nil
}

func (b *Builder) cachePath(root, rel, sha string) string {
	sum := sha256.Sum256([]byte(root + "\x00" + rel))
	return filepath.Join(b.CacheDir, hex.EncodeToString(sum[:8]), "v"+extract.Version+"-"+sha+".json")
}

func (b *Builder) build(ctx context.Context, root, rel, sha string) (*model.Graph, error) {
	tmp, err := os.MkdirTemp("", "archdiag-base-")
	if err != nil {
		return nil, fmt.Errorf("create worktree dir: %w", err)
	}
	defer os.RemoveAll(tmp)

	wt := filepath.Join(tmp, "worktree")
	if _, err := git(ctx, root, "worktree", "add", "--detach", wt, sha); err != nil {
		return nil, err
	}
	defer func() {
		// Clean up even if ctx was cancelled mid-extraction.
		_, _ = git(context.WithoutCancel(ctx), root, "worktree", "remove", "--force", wt)
	}()

	g, err := b.Extractor.Extract(ctx, filepath.Join(wt, rel))
	if err != nil {
		return nil, fmt.Errorf("extract base at %.12s: %w", sha, err)
	}
	return g, nil
}

// relToRoot returns dir relative to the repository root, resolving symlinks
// on both sides because git reports the resolved root.
func relToRoot(root, dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", dir, err)
	}
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		abs = r
	}
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", fmt.Errorf("locate %s in repository %s: %w", dir, root, err)
	}
	return rel, nil
}

func readCache(path string) (*model.Graph, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return model.Read(f)
}

func writeCache(path string, g *model.Graph) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := g.Write(tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}
