// Package extract builds a model.Graph from source code.
package extract

import (
	"context"
	"errors"
	"fmt"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/packages"

	"codebase/arch/internal/model"
)

// Version identifies the extractor's output. Bump it whenever the graphs it
// produces change, so cached base graphs are rebuilt.
const Version = "1"

// Extractor turns the module rooted at (or containing) dir into a graph.
type Extractor interface {
	Extract(ctx context.Context, dir string) (*model.Graph, error)
}

// GoExtractor extracts Go packages with golang.org/x/tools/go/packages.
type GoExtractor struct{}

const loadMode = packages.NeedName | packages.NeedFiles | packages.NeedImports |
	packages.NeedModule | packages.NeedTypes

// Extract loads every package matching ./... in dir. Packages with type
// errors are kept; their errors are recorded on the node.
func (GoExtractor) Extract(ctx context.Context, dir string) (*model.Graph, error) {
	cfg := &packages.Config{Context: ctx, Dir: dir, Mode: loadMode}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("load packages in %s: %w", dir, err)
	}
	mainMod := mainModule(pkgs)
	if mainMod == nil {
		// A module with no packages yet (a new project) has an empty graph.
		mod, err := modulePath(ctx, dir)
		if err != nil {
			return nil, fmt.Errorf("no packages of a main module found in %s: %w", dir, err)
		}
		return model.NewGraph(mod), nil
	}

	g := model.NewGraph(mainMod.Path)
	for _, p := range pkgs {
		if p.PkgPath == "" || p.Module == nil || !p.Module.Main {
			continue
		}
		g.Nodes[p.PkgPath] = localNode(mainMod, p)
		for path, imp := range p.Imports {
			if path == "C" {
				continue
			}
			if g.Nodes[path] == nil {
				g.Nodes[path] = importedNode(mainMod.Path, path, imp)
			}
			g.Edges = append(g.Edges, model.Edge{From: p.PkgPath, To: path})
		}
	}
	g.Normalize()
	return g, nil
}

// modulePath reads the module path from the go.mod governing dir.
func modulePath(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "go", "env", "GOMOD")
	cmd.Dir = dir
	out, err := cmd.Output()
	gomod := strings.TrimSpace(string(out))
	if err != nil || gomod == "" || gomod == os.DevNull {
		return "", errors.New("not inside a Go module")
	}
	data, err := os.ReadFile(gomod)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", gomod, err)
	}
	path := modfile.ModulePath(data)
	if path == "" {
		return "", fmt.Errorf("%s declares no module path", gomod)
	}
	return path, nil
}

func mainModule(pkgs []*packages.Package) *packages.Module {
	for _, p := range pkgs {
		if p.Module != nil && p.Module.Main {
			return p.Module
		}
	}
	return nil
}

func localNode(mod *packages.Module, p *packages.Package) *model.Node {
	n := &model.Node{ID: p.PkgPath, Kind: model.Local, Dir: relDir(mod.Path, p.PkgPath)}
	for _, f := range p.GoFiles {
		n.Files = append(n.Files, filepath.Base(f))
	}
	if p.Types != nil {
		for _, name := range p.Types.Scope().Names() {
			if token.IsExported(name) {
				n.Exports = append(n.Exports, name)
			}
		}
	}
	n.Errors = errorMessages(mod.Dir, p.Errors)
	return n
}

// importedNode describes an import that is not (yet) one of the loaded
// packages. Without NeedDeps, imp carries only its ID and module.
func importedNode(mod, path string, imp *packages.Package) *model.Node {
	switch {
	case path == mod || strings.HasPrefix(path, mod+"/"):
		return &model.Node{ID: path, Kind: model.Local, Dir: relDir(mod, path)}
	case isStd(path):
		return &model.Node{ID: path, Kind: model.Std}
	default:
		n := &model.Node{ID: path, Kind: model.External}
		if imp != nil && imp.Module != nil {
			n.Module = imp.Module.Path
		}
		return n
	}
}

// relDir is the module-relative directory of a local import path.
func relDir(mod, path string) string {
	return strings.TrimPrefix(strings.TrimPrefix(path, mod), "/")
}

// isStd reports whether path looks like a standard-library import path:
// its first element contains no dot.
func isStd(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

// errorMessages keeps type errors when there are any (the go command reports
// the same problem again as a list error) and makes paths module-relative.
func errorMessages(modDir string, errs []packages.Error) []string {
	hasType := slices.ContainsFunc(errs, func(e packages.Error) bool { return e.Kind == packages.TypeError })
	var out []string
	for _, e := range errs {
		if hasType && e.Kind != packages.TypeError {
			continue
		}
		out = append(out, strings.TrimPrefix(e.Error(), modDir+string(filepath.Separator)))
	}
	return out
}
