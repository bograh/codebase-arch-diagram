# archdiag

Live architecture diagrams for Go modules. `archdiag` draws your packages and
their imports, grouped by directory, and highlights what your working tree
changes compared with a git ref: new packages and imports in green, removed
in red, packages whose exported API or file list changed in amber.

## Install

```sh
go install codebase/arch/cmd/archdiag@latest   # or: go build ./cmd/archdiag
```

Requires Go 1.26+ and `git` on your PATH. No Graphviz install is needed; it's
embedded as WebAssembly.

## Use

```sh
archdiag serve [flags] [dir]    # live diagram at http://127.0.0.1:7777
archdiag render [flags] [dir] > diff.svg
```

| Flag | Default | Meaning |
|---|---|---|
| `--base` | `main` | git ref to compare with |
| `--std` | off | show standard-library packages |
| `--deps` | `collapsed` | third-party packages: `collapsed` (one node per module), `hidden`, `full` |
| `--addr` | `127.0.0.1:7777` | listen address (`serve` only) |

Flags go before `dir`. In the browser: scroll to zoom, drag to pan,
double-click to reset, click a package for details, and use Diff / Base /
Current to switch views. Base graphs are cached by commit in your user cache
directory (`~/.cache/archdiag` on Linux).

## Develop

```sh
go generate ./internal/web   # after editing .templ files
go test ./...
go test ./cmd/archdiag -run TestRenderGolden -update   # after intended diagram changes
```
