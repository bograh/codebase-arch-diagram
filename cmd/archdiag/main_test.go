package main

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=test", "-c", "user.email=test@example.com",
		"-c", "commit.gpgsign=false"}, args...)
	if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// shopRepo commits a base with main -> db and legacy, then edits the working
// tree: adds api (main -> api -> db), deletes legacy, adds db.Close.
func shopRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	write(t, dir, "go.mod", "module example.com/shop\n\ngo 1.26\n")
	write(t, dir, "main.go", "package main\n\nimport _ \"example.com/shop/internal/db\"\n\nfunc main() {}\n")
	write(t, dir, "internal/db/db.go", "package db\n\nfunc Open() {}\n")
	write(t, dir, "internal/legacy/legacy.go", "package legacy\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-q", "-m", "base")

	write(t, dir, "internal/api/api.go", "package api\n\nimport _ \"example.com/shop/internal/db\"\n\nfunc Serve() {}\n")
	write(t, dir, "main.go", "package main\n\nimport _ \"example.com/shop/internal/api\"\n\nfunc main() {}\n")
	write(t, dir, "internal/db/db.go", "package db\n\nfunc Open() {}\n\nfunc Close() {}\n")
	if err := os.RemoveAll(filepath.Join(dir, "internal/legacy")); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRenderGolden(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	dir := shopRepo(t)

	var out, errOut bytes.Buffer
	if code := run([]string{"render", "--base", "main", dir}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{
		`id="n-example.com/shop/internal/api" class="node st-added`,
		`id="n-example.com/shop/internal/legacy" class="node st-removed`,
		`id="n-example.com/shop/internal/db" class="node st-changed`,
		`id="e-example.com/shop|example.com/shop/internal/db" class="edge st-removed"`,
		`id="e-example.com/shop|example.com/shop/internal/api" class="edge st-added"`,
		`<style>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("render output missing %q", want)
		}
	}

	golden := filepath.Join("testdata", "render.golden.svg")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, out.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden file (run with -update to create it): %v", err)
	}
	if got != string(want) {
		t.Errorf("render output differs from %s; rerun with -update if the change is intended", golden)
	}
}

func TestRenderRequiresBase(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "go.mod", "module example.com/nogit\n\ngo 1.26\n")
	write(t, dir, "main.go", "package main\n\nfunc main() {}\n")

	var out, errOut bytes.Buffer
	if code := run([]string{"render", dir}, &out, &errOut); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "resolve base") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestUsageErrors(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{nil, 2, "Usage:"},
		{[]string{"frobnicate"}, 2, `unknown command "frobnicate"`},
		{[]string{"render", "--deps", "everything", "."}, 1, "invalid deps mode"},
		{[]string{"render", "a", "b"}, 1, "at most one directory"},
	} {
		var out, errOut bytes.Buffer
		code := run(tc.args, &out, &errOut)
		if code != tc.code || !strings.Contains(errOut.String(), tc.want) {
			t.Errorf("run(%v) = %d, stderr %q; want %d containing %q", tc.args, code, errOut.String(), tc.code, tc.want)
		}
	}
	var out bytes.Buffer
	if code := run([]string{"help"}, &out, &out); code != 0 || !strings.Contains(out.String(), "archdiag serve") {
		t.Errorf("help = %d, %q", code, out.String())
	}
}
