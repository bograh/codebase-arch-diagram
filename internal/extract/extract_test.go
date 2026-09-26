package extract

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"codebase/arch/internal/model"
)

func extract(t *testing.T, dir string) *model.Graph {
	t.Helper()
	g, err := GoExtractor{}.Extract(context.Background(), dir)
	if err != nil {
		t.Fatalf("Extract(%s): %v", dir, err)
	}
	return g
}

func TestExtractLayered(t *testing.T) {
	g := extract(t, "testdata/layered")

	if g.Module != "example.com/layered" {
		t.Errorf("module = %q", g.Module)
	}
	wantKinds := map[string]model.NodeKind{
		"example.com/layered":              model.Local,
		"example.com/layered/internal/api": model.Local,
		"example.com/layered/internal/db":  model.Local,
		"fmt":                              model.Std,
		"example.com/dep/strutil":          model.External,
	}
	gotKinds := map[string]model.NodeKind{}
	for id, n := range g.Nodes {
		gotKinds[id] = n.Kind
	}
	if !reflect.DeepEqual(gotKinds, wantKinds) {
		t.Errorf("nodes = %v, want %v", gotKinds, wantKinds)
	}

	root := g.Nodes["example.com/layered"]
	if root.Dir != "" {
		t.Errorf("root dir = %q, want empty", root.Dir)
	}
	api := g.Nodes["example.com/layered/internal/api"]
	if api.Dir != "internal/api" {
		t.Errorf("api dir = %q", api.Dir)
	}
	if want := []string{"api.go"}; !reflect.DeepEqual(api.Files, want) {
		t.Errorf("api files = %v, want %v", api.Files, want)
	}
	if want := []string{"New", "Server"}; !reflect.DeepEqual(api.Exports, want) {
		t.Errorf("api exports = %v, want %v", api.Exports, want)
	}
	if len(api.Errors) != 0 {
		t.Errorf("api errors = %v", api.Errors)
	}
	if dep := g.Nodes["example.com/dep/strutil"]; dep.Module != "example.com/dep" {
		t.Errorf("dep module = %q, want example.com/dep", dep.Module)
	}

	wantEdges := []model.Edge{
		{From: "example.com/layered", To: "example.com/layered/internal/api"},
		{From: "example.com/layered", To: "fmt"},
		{From: "example.com/layered/internal/api", To: "example.com/dep/strutil"},
		{From: "example.com/layered/internal/api", To: "example.com/layered/internal/db"},
	}
	if !reflect.DeepEqual(g.Edges, wantEdges) {
		t.Errorf("edges = %v, want %v", g.Edges, wantEdges)
	}
}

func TestExtractKeepsPackagesWithTypeErrors(t *testing.T) {
	g := extract(t, "testdata/broken")

	bad := g.Nodes["example.com/broken/bad"]
	if bad == nil {
		t.Fatal("bad package missing")
	}
	if len(bad.Errors) == 0 {
		t.Fatal("bad package has no errors")
	}
	for _, msg := range bad.Errors {
		if !strings.HasPrefix(msg, "bad/bad.go") {
			t.Errorf("error %q should start with a module-relative path", msg)
		}
	}
	if !strings.Contains(strings.Join(bad.Errors, "\n"), "strings.Nope") {
		t.Errorf("errors %v do not mention strings.Nope", bad.Errors)
	}
	if want := []string{"F"}; !reflect.DeepEqual(bad.Exports, want) {
		t.Errorf("bad exports = %v, want %v", bad.Exports, want)
	}
	if ok := g.Nodes["example.com/broken/ok"]; ok == nil || len(ok.Errors) != 0 {
		t.Errorf("ok package = %+v, want present without errors", ok)
	}
	if !contains(g.Edges, model.Edge{From: "example.com/broken/bad", To: "strings"}) {
		t.Errorf("edges %v missing bad -> strings", g.Edges)
	}
}

func TestExtractSurvivesImportCycle(t *testing.T) {
	g := extract(t, "testdata/cycle")

	a, b := g.Nodes["example.com/cycle/a"], g.Nodes["example.com/cycle/b"]
	if a == nil || b == nil {
		t.Fatalf("nodes = %v, want a and b", g.SortedIDs())
	}
	if len(a.Errors)+len(b.Errors) == 0 {
		t.Error("expected an import cycle error on a or b")
	}
}

func TestExtractFailsOutsideModule(t *testing.T) {
	if _, err := (GoExtractor{}).Extract(context.Background(), t.TempDir()); err == nil {
		t.Error("Extract succeeded outside a module, want error")
	}
}

func contains(edges []model.Edge, e model.Edge) bool {
	for _, x := range edges {
		if x == e {
			return true
		}
	}
	return false
}

func TestExtractModuleWithoutPackages(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/empty\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := extract(t, dir)
	if g.Module != "example.com/empty" || len(g.Nodes) != 0 {
		t.Errorf("graph = %+v, want an empty graph for example.com/empty", g)
	}
}
