// Package layout positions a diff graph for drawing. It is the only package
// that knows about Graphviz; callers get plain coordinates.
package layout

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/goccy/go-graphviz"
	"github.com/goccy/go-graphviz/cgraph"

	"codebase/arch/internal/diff"
	"codebase/arch/internal/model"
)

// NodeBox is a positioned package. X, Y is the top-left corner in SVG user
// units, with y growing downwards.
type NodeBox struct {
	ID, Label  string
	X, Y, W, H float64
	Status     diff.Status
	Kind       model.NodeKind
	HasErrors  bool
}

// ClusterBox is a positioned directory cluster.
type ClusterBox struct {
	Path, Label string
	X, Y, W, H  float64
	Touched     bool
}

// EdgePath is a routed import. D is SVG path data ending at the arrow tip.
type EdgePath struct {
	From, To string
	D        string
	Status   diff.Status
}

// Layout is a drawable diagram. Nodes are sorted by ID; Clusters and Edges
// follow the order of the DiffGraph they came from.
type Layout struct {
	Width, Height float64
	Nodes         []NodeBox
	Clusters      []ClusterBox
	Edges         []EdgePath
}

const (
	nodeHeight   = 36.0 // points
	minNodeWidth = 90.0
	charWidth    = 7.2 // approximate width of one 12px label character
	labelPadding = 28.0
	clusterPad   = "20" // room for the cluster title, in points
)

// Engine runs Graphviz (compiled to WebAssembly). It is safe for concurrent
// use; layouts run one at a time.
type Engine struct {
	mu sync.Mutex
	gv *graphviz.Graphviz
}

// New starts a Graphviz instance. Starting one is slow, so reuse the Engine.
func New(ctx context.Context) (*Engine, error) {
	gv, err := graphviz.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("start graphviz: %w", err)
	}
	return &Engine{gv: gv}, nil
}

// Close releases the Graphviz instance.
func (e *Engine) Close() error { return e.gv.Close() }

// Layout positions every node, cluster and edge of d, including removed
// ones, so that switching between base and current views never moves anything.
func (e *Engine) Layout(ctx context.Context, d *diff.DiffGraph) (*Layout, error) {
	if len(d.Nodes) == 0 {
		return &Layout{}, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	g, err := e.gv.Graph()
	if err != nil {
		return nil, fmt.Errorf("new graph: %w", err)
	}
	defer g.Close()

	b := &builder{d: d, g: g,
		clusters: map[string]*cgraph.Graph{},
		nodes:    map[string]*cgraph.Node{},
		widths:   map[string]float64{},
		edges:    map[model.Edge]*cgraph.Edge{},
	}
	if err := b.build(); err != nil {
		return nil, err
	}
	if err := e.gv.Render(ctx, g, graphviz.XDOT, io.Discard); err != nil {
		return nil, fmt.Errorf("graphviz layout: %w", err)
	}
	return b.read()
}

type attrSetter interface {
	SafeSet(name, value, def string) error
}

func setAttrs(obj attrSetter, kv ...string) error {
	for i := 0; i+1 < len(kv); i += 2 {
		if err := obj.SafeSet(kv[i], kv[i+1], ""); err != nil {
			return fmt.Errorf("set %s=%s: %w", kv[i], kv[i+1], err)
		}
	}
	return nil
}

type builder struct {
	d        *diff.DiffGraph
	g        *cgraph.Graph
	clusters map[string]*cgraph.Graph
	nodes    map[string]*cgraph.Node
	widths   map[string]float64
	edges    map[model.Edge]*cgraph.Edge
}

func (b *builder) build() error {
	if err := setAttrs(b.g, "rankdir", "TB", "nodesep", "0.35", "ranksep", "0.7"); err != nil {
		return err
	}
	for _, c := range b.d.Clusters {
		parent := b.g
		if c.Parent != "" {
			parent = b.clusters[c.Parent]
		}
		sg, err := parent.CreateSubGraphByName("cluster_" + c.Path)
		if err != nil {
			return fmt.Errorf("create cluster %s: %w", c.Path, err)
		}
		if err := setAttrs(sg, "margin", clusterPad); err != nil {
			return err
		}
		b.clusters[c.Path] = sg
		for _, id := range c.Nodes {
			if err := b.addNode(sg, id); err != nil {
				return err
			}
		}
	}
	for _, id := range b.d.RootNodes {
		if err := b.addNode(b.g, id); err != nil {
			return err
		}
	}
	for _, e := range b.d.Edges {
		from, to := b.nodes[e.From], b.nodes[e.To]
		if from == nil || to == nil {
			return fmt.Errorf("edge %s -> %s references a node outside the graph", e.From, e.To)
		}
		ge, err := b.g.CreateEdgeByName(e.From+"->"+e.To, from, to)
		if err != nil {
			return fmt.Errorf("create edge %s -> %s: %w", e.From, e.To, err)
		}
		b.edges[e.Edge] = ge
	}
	return nil
}

func (b *builder) addNode(container *cgraph.Graph, id string) error {
	n, err := container.CreateNodeByName(id)
	if err != nil {
		return fmt.Errorf("create node %s: %w", id, err)
	}
	w := nodeWidth(b.d.Nodes[id].Label(b.d.Module))
	b.nodes[id], b.widths[id] = n, w
	return setAttrs(n, "shape", "box", "fixedsize", "true", "label", "",
		"width", num(w/72), "height", num(nodeHeight/72))
}

func (b *builder) read() (*Layout, error) {
	bb, err := parseBox(b.g.GetStr("bb"))
	if err != nil {
		return nil, fmt.Errorf("graph bounding box: %w", err)
	}
	height := bb[3]
	flip := func(y float64) float64 { return height - y }
	l := &Layout{Width: bb[2], Height: height}

	for _, c := range b.d.Clusters {
		box, err := parseBox(b.clusters[c.Path].GetStr("bb"))
		if err != nil {
			return nil, fmt.Errorf("cluster %s bounding box: %w", c.Path, err)
		}
		l.Clusters = append(l.Clusters, ClusterBox{
			Path: c.Path, Label: c.Label, Touched: c.Touched,
			X: box[0], Y: flip(box[3]), W: box[2] - box[0], H: box[3] - box[1],
		})
	}

	for _, id := range sortedKeys(b.nodes) {
		x, y, err := parsePoint(b.nodes[id].GetStr("pos"))
		if err != nil {
			return nil, fmt.Errorf("node %s position: %w", id, err)
		}
		dn, w := b.d.Nodes[id], b.widths[id]
		l.Nodes = append(l.Nodes, NodeBox{
			ID: id, Label: dn.Label(b.d.Module),
			X: x - w/2, Y: flip(y) - nodeHeight/2, W: w, H: nodeHeight,
			Status: dn.Status, Kind: dn.Kind, HasErrors: len(dn.Errors) > 0,
		})
	}

	for _, e := range b.d.Edges {
		d, err := splinePath(b.edges[e.Edge].GetStr("pos"), flip)
		if err != nil {
			return nil, fmt.Errorf("edge %s -> %s: %w", e.From, e.To, err)
		}
		l.Edges = append(l.Edges, EdgePath{From: e.From, To: e.To, D: d, Status: e.Status})
	}
	return l, nil
}

func nodeWidth(label string) float64 {
	return max(minNodeWidth, float64(len([]rune(label)))*charWidth+labelPadding)
}

// splinePath converts a Graphviz edge pos into SVG path data, flipping y.
func splinePath(pos string, flip func(float64) float64) (string, error) {
	var start, end *[2]float64
	var pts [][2]float64
	for _, tok := range strings.Fields(pos) {
		var kind byte
		if strings.HasPrefix(tok, "e,") || strings.HasPrefix(tok, "s,") {
			kind, tok = tok[0], tok[2:]
		}
		x, y, err := parsePoint(tok)
		if err != nil {
			return "", fmt.Errorf("edge pos %q: %w", pos, err)
		}
		p := [2]float64{x, flip(y)}
		switch kind {
		case 'e':
			end = &p
		case 's':
			start = &p
		default:
			pts = append(pts, p)
		}
	}
	if len(pts) < 4 || (len(pts)-1)%3 != 0 {
		return "", fmt.Errorf("edge pos %q: want 3n+1 spline points, got %d", pos, len(pts))
	}
	var sb strings.Builder
	if start != nil {
		sb.WriteString("M" + pt(*start) + " L" + pt(pts[0]))
	} else {
		sb.WriteString("M" + pt(pts[0]))
	}
	for i := 1; i < len(pts); i += 3 {
		sb.WriteString(" C" + pt(pts[i]) + " " + pt(pts[i+1]) + " " + pt(pts[i+2]))
	}
	if end != nil {
		sb.WriteString(" L" + pt(*end))
	}
	return sb.String(), nil
}

func parsePoint(s string) (x, y float64, err error) {
	xs, ys, ok := strings.Cut(s, ",")
	if !ok {
		return 0, 0, fmt.Errorf("bad point %q", s)
	}
	if x, err = strconv.ParseFloat(xs, 64); err != nil {
		return 0, 0, fmt.Errorf("bad point %q: %w", s, err)
	}
	if y, err = strconv.ParseFloat(ys, 64); err != nil {
		return 0, 0, fmt.Errorf("bad point %q: %w", s, err)
	}
	return x, y, nil
}

// parseBox parses a Graphviz "llx,lly,urx,ury" box.
func parseBox(s string) ([4]float64, error) {
	var box [4]float64
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return box, fmt.Errorf("bad box %q", s)
	}
	for i, p := range parts {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return box, fmt.Errorf("bad box %q: %w", s, err)
		}
		box[i] = v
	}
	return box, nil
}

func pt(p [2]float64) string { return num(p[0]) + "," + num(p[1]) }

func num(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
