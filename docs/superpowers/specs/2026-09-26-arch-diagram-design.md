# archdiag — Live Architecture Diagrams for Go Codebases

**Date:** 2026-09-26
**Status:** Approved design, pending implementation plan

## Goal

A local tool that turns a Go codebase into a package-level architecture diagram,
keeps it updated live as files change, and shows how the working tree differs
architecturally from a git base ref (default `main`).

## Scope

**In v1**

- Go codebases only (single module, the one containing the target dir).
- Package-level graph: nodes are packages, edges are imports, clusters are directories.
- Diff of working tree (including uncommitted changes) against a git ref.
- Live updates in the browser as files are saved.
- Headless `render` command producing a standalone SVG.

**Out of v1** (the design leaves room for them)

- Type-level drill-down (structs/interfaces inside a package).
- Other languages (the `Extractor` interface is the extension point).
- Timeline / session history of snapshots.
- Rename detection (a rename appears as removed + added).
- Layout-stability position hints across updates.

## CLI

```
archdiag serve  [--base main] [--addr 127.0.0.1:7777] [--std] [--deps=collapsed|hidden|full] [dir]
archdiag render [--base main] [--std] [--deps=collapsed|hidden|full] [dir] > diff.svg
```

- `dir` defaults to `.`.
- `--std` shows standard-library packages (hidden by default).
- `--deps` controls third-party packages: `collapsed` (default) shows one node per
  module (e.g. `github.com/jackc/pgx/v5`); `hidden` omits them; `full` shows every package.
- CLI parsing uses stdlib `flag` with manual subcommand dispatch.

## Components

| Package | Responsibility | Depends on |
|---|---|---|
| `cmd/archdiag` | CLI parsing, wiring, subcommand dispatch | all internal packages |
| `internal/model` | Pure data types: `Graph`, `Node`, `Edge`, JSON (de)serialisation | nothing |
| `internal/extract` | Go source dir → `model.Graph` via `golang.org/x/tools/go/packages`; defines the `Extractor` interface | `model` |
| `internal/gitbase` | Resolve ref → SHA; build base graph in a temp `git worktree`; cache graph JSON by SHA | `model`, `extract`, `git` binary |
| `internal/diff` | `(base, current Graph)` → `DiffGraph` (union with per-element status) | `model` |
| `internal/layout` | `DiffGraph` → `Layout` (plain coordinates); only importer of go-graphviz | `diff`, go-graphviz |
| `internal/watch` | Recursive fsnotify watcher, filtered and debounced | fsnotify |
| `internal/server` | HTTP handlers, SSE hub, pipeline worker, current state | all of the above, `web` |
| `internal/web` | templ components and embedded static assets | `layout`, `diff` |

Boundary rules:

- `model` is the contract between `extract`, `diff`, and `layout`. Each is testable with hand-built graphs.
- `layout` exposes no Graphviz types. Swapping the layout engine touches only this package.
- `render` runs the same pipeline as `serve` once, then writes the templ SVG component to stdout.

### Core types (sketch)

```go
// model
type Node struct {
    ID      string   // import path (or module path for collapsed deps)
    Kind    NodeKind // Local | Std | External
    Dir     string   // module-relative directory ("" for non-local)
    Module  string   // owning module path (external only; used to collapse deps)
    Files   []string // sorted .go file names (local only)
    Exports []string // sorted exported identifiers (local only)
    Errors  []string // load/type errors for this package
}
type Edge struct{ From, To string }
type Graph struct {
    Module string
    Nodes  map[string]*Node
    Edges  []Edge // sorted, deduplicated
}

// extract
type Extractor interface {
    Extract(ctx context.Context, dir string) (*model.Graph, error)
}

// diff
type Status int // Same | Added | Removed | Changed
type DiffGraph struct {
    Nodes    map[string]DiffNode // node + Status + (for Changed) export/file deltas
    Edges    []DiffEdge          // edge + Status
    Clusters []Cluster           // directory tree; Touched if any descendant is not Same
}

// layout
type Layout struct {
    Width, Height float64
    Nodes    []NodeBox    // ID, X, Y, W, H, Status
    Clusters []ClusterBox // Path, X, Y, W, H, Touched
    Edges    []EdgePath   // From, To, SVG path "d", Status
}
```

## Data Flow

### Startup

1. `git rev-parse <base>` → SHA.
2. Base graph: cache hit at `$XDG_CACHE_HOME/archdiag/<module-hash>/<sha>.json` → load;
   otherwise `git worktree add --detach <tmp> <sha>`, extract, write cache, remove worktree.
   The cache key also includes the archdiag extractor version. The cache holds the
   unfiltered graph; `--std`/`--deps` filtering is applied after loading, so changing
   flags never invalidates the cache.
3. Extract the current working tree.
4. `diff` → `layout` → store as current state.
5. Serve `GET /` (full templ page), `GET /events` (SSE), `GET /node/{id}` (details panel), `/static/*`.

### On change

```
fsnotify → filter (*.go, go.mod, go.sum; skip .git, vendor, testdata) → debounce 300ms
         → [single worker] re-resolve base SHA (rebuild base if it moved)
         → extract current → diff → layout → render templ <svg> fragment → SSE broadcast
```

- Exactly one pipeline run at a time. An event arriving mid-run sets a dirty flag; the
  worker runs once more afterwards. Events never queue up.
- New directories are added to the watcher as they appear.
- The browser uses the htmx SSE extension with `hx-swap="morph"` (idiomorph), so existing
  SVG elements update in place and CSS transitions animate moved nodes.

## Diff Model

- Node identity: import path. Edge identity: `(From, To)`.
- Node status:
  - **Added**: in current only. **Removed**: in base only.
  - **Changed**: in both, and `Exports` or `Files` differ.
  - **Same**: otherwise. Internal-only edits are deliberately `Same` to avoid noise.
- Edge status: Added / Removed / Same.
- Clusters mirror the directory tree of local packages; a cluster is **Touched** if any
  descendant node or edge is not Same.
- Layout is computed on the **union** graph, so removed elements keep positions and
  render as red ghosts; added render green; changed render amber.

## UI

- Page shell, diagram, and panels are templ components. The diagram `<svg>` is emitted
  element-by-element by templ from `Layout` coordinates (not Graphviz's SVG output).
- **Diff / Base / Current** toggle flips `data-view` on the root `<svg>`; CSS hides added
  or removed elements. No server round trip, no re-layout.
- Sidebar summary: counts and lists of added/removed/changed packages and edges.
  Clicking an item pans to and highlights the element.
- Clicking a node: htmx `GET /node/{id}` → details panel (imports, importers, export and
  file deltas, errors).
- Pan/zoom: ~50 lines of vanilla JS on the SVG `viewBox`, preserved across morphs.
- Styling: plain CSS with custom properties for diff colours, light and dark themes.
- Assets (htmx 2, htmx-ext-sse, idiomorph, CSS, pan/zoom JS) are vendored and embedded
  with `embed`. The binary is self-contained; no JS build step.

## Error Handling

Principle: always show the last good diagram, never a blank screen.

- **Packages with type errors:** `go/packages` still yields imports; keep the node, attach
  `Errors`, render a ⚠ badge; details panel lists errors.
- **Total extraction failure** (e.g. malformed `go.mod`): keep the previous layout,
  broadcast a "stale: <error>" banner.
- **Invalid base ref / not a git repo:** fatal for `render`; `serve` falls back to
  no-base mode (current graph only, all nodes Same) with a banner.
- **Worktree hygiene:** worktrees live under a temp dir with deferred
  `git worktree remove --force`; `git worktree prune` runs at startup.
- **SSE:** htmx reconnects automatically; the server sends current state on every new connection.
- **Base cache corruption:** unreadable cache file is deleted and rebuilt.

## Testing

- `model`, `diff`: table-driven unit tests with hand-built graphs.
- `extract`: fixture modules under `testdata/` — clean layered module, import cycle,
  package with a type error, module with third-party deps (vendored fixture, no network).
- `gitbase`: throwaway git repos in `t.TempDir()`; covers cache hit/miss and moved base.
- `layout`: property tests — every node has a box, every node box lies within its
  cluster box, every edge endpoint has a node. No exact-coordinate assertions.
- `watch`: debounce and filter behaviour with a temp dir.
- End-to-end: golden SVGs from `archdiag render` against fixture repos, updated with `-update`.
- `server`: `httptest` server; edit a fixture file; assert an SSE event arrives whose
  fragment carries the expected diff classes.

## Tech Stack

| Concern | Choice |
|---|---|
| Language | Go 1.26 |
| Analysis | `golang.org/x/tools/go/packages` |
| Layout | `github.com/goccy/go-graphviz` (WebAssembly, no cgo) |
| UI | `github.com/a-h/templ`; htmx 2 + SSE extension + idiomorph (vendored, embedded) |
| Styling | Plain CSS, custom properties, no build step |
| File watching | `github.com/fsnotify/fsnotify` |
| CLI | stdlib `flag` |
| Git | `git` binary via `os/exec` |
