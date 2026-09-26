package layout

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"codebase/arch/internal/diff"
	"codebase/arch/internal/model"
)

const mod = "example.com/m"

func pkg(dir string) string {
	if dir == "" {
		return mod
	}
	return mod + "/" + dir
}

func local(dir string) *model.Node {
	return &model.Node{ID: pkg(dir), Kind: model.Local, Dir: dir}
}

// sample has nested clusters, a std dependency, and added/removed elements.
func sample() *diff.DiffGraph {
	base, cur := model.NewGraph(mod), model.NewGraph(mod)
	for _, dir := range []string{"", "cmd/app", "internal/api", "internal/db"} {
		base.Nodes[pkg(dir)] = local(dir)
		cur.Nodes[pkg(dir)] = local(dir)
	}
	cur.Nodes[pkg("internal/api/v2")] = local("internal/api/v2")
	base.Nodes["fmt"] = &model.Node{ID: "fmt", Kind: model.Std}
	cur.Nodes["fmt"] = &model.Node{ID: "fmt", Kind: model.Std}
	base.Edges = []model.Edge{
		{From: pkg("cmd/app"), To: pkg("internal/api")},
		{From: pkg("internal/api"), To: pkg("internal/db")},
		{From: pkg(""), To: "fmt"},
	}
	cur.Edges = []model.Edge{
		{From: pkg("cmd/app"), To: pkg("internal/api/v2")},
		{From: pkg("internal/api/v2"), To: pkg("internal/db")},
		{From: pkg("internal/api"), To: pkg("internal/db")},
		{From: pkg(""), To: "fmt"},
	}
	base.Normalize()
	cur.Normalize()
	return diff.Compute(base, cur)
}

func newEngine(t *testing.T) *Engine {
	t.Helper()
	e, err := New(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	return e
}

func layoutOf(t *testing.T, e *Engine, d *diff.DiffGraph) *Layout {
	t.Helper()
	l, err := e.Layout(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

const eps = 1.0

type rect struct{ x, y, w, h float64 }

func inside(inner, outer rect) bool {
	return inner.x >= outer.x-eps && inner.y >= outer.y-eps &&
		inner.x+inner.w <= outer.x+outer.w+eps && inner.y+inner.h <= outer.y+outer.h+eps
}

func TestEveryNodeGetsABoxOnTheCanvas(t *testing.T) {
	d := sample()
	l := layoutOf(t, newEngine(t), d)

	if l.Width <= 0 || l.Height <= 0 {
		t.Fatalf("canvas = %vx%v", l.Width, l.Height)
	}
	if len(l.Nodes) != len(d.Nodes) {
		t.Fatalf("got %d boxes for %d nodes", len(l.Nodes), len(d.Nodes))
	}
	canvas := rect{0, 0, l.Width, l.Height}
	for _, n := range l.Nodes {
		dn := d.Nodes[n.ID]
		if dn == nil {
			t.Errorf("box for unknown node %s", n.ID)
			continue
		}
		if n.Status != dn.Status || n.Kind != dn.Kind || n.Label != dn.Label(mod) {
			t.Errorf("box %s = %+v does not match node %+v", n.ID, n, dn)
		}
		if n.W <= 0 || n.H <= 0 || !inside(rect{n.X, n.Y, n.W, n.H}, canvas) {
			t.Errorf("box %s = %+v is not on the canvas", n.ID, n)
		}
	}
}

func TestNodesAndChildClustersSitInsideTheirCluster(t *testing.T) {
	d := sample()
	l := layoutOf(t, newEngine(t), d)

	boxes := map[string]rect{}
	for _, n := range l.Nodes {
		boxes["n:"+n.ID] = rect{n.X, n.Y, n.W, n.H}
	}
	for _, c := range l.Clusters {
		boxes["c:"+c.Path] = rect{c.X, c.Y, c.W, c.H}
	}
	if len(l.Clusters) != len(d.Clusters) {
		t.Fatalf("got %d cluster boxes for %d clusters", len(l.Clusters), len(d.Clusters))
	}
	for _, c := range d.Clusters {
		outer := boxes["c:"+c.Path]
		for _, id := range c.Nodes {
			if !inside(boxes["n:"+id], outer) {
				t.Errorf("node %s %+v escapes cluster %s %+v", id, boxes["n:"+id], c.Path, outer)
			}
		}
		for _, child := range c.Children {
			if !inside(boxes["c:"+child], outer) {
				t.Errorf("cluster %s escapes cluster %s", child, c.Path)
			}
		}
	}
}

func TestEveryEdgeGetsAPath(t *testing.T) {
	d := sample()
	l := layoutOf(t, newEngine(t), d)

	if len(l.Edges) != len(d.Edges) {
		t.Fatalf("got %d paths for %d edges", len(l.Edges), len(d.Edges))
	}
	for i, e := range l.Edges {
		want := d.Edges[i]
		if e.From != want.From || e.To != want.To || e.Status != want.Status {
			t.Errorf("path %d = %+v, want edge %+v", i, e, want)
		}
		if !strings.HasPrefix(e.D, "M") || !strings.Contains(e.D, " C") {
			t.Errorf("path %s->%s has data %q", e.From, e.To, e.D)
		}
	}
}

func TestLayoutIsDeterministic(t *testing.T) {
	e := newEngine(t)
	a := layoutOf(t, e, sample())
	b := layoutOf(t, e, sample())
	if !reflect.DeepEqual(a, b) {
		t.Error("two layouts of the same graph differ")
	}
}

func TestEmptyGraph(t *testing.T) {
	empty := model.NewGraph(mod)
	l := layoutOf(t, newEngine(t), diff.Compute(empty, empty))
	if l.Width != 0 || len(l.Nodes) != 0 {
		t.Errorf("layout = %+v, want empty", l)
	}
}

func TestSplinePath(t *testing.T) {
	flip := func(y float64) float64 { return 20 - y }
	got, err := splinePath("e,10,0 0,10 3,10 6,10 9,10", flip)
	if err != nil {
		t.Fatal(err)
	}
	if want := "M0.0,10.0 C3.0,10.0 6.0,10.0 9.0,10.0 L10.0,20.0"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
	for _, bad := range []string{"", "1,2 3,4", "e,1,2", "x,y 1,2 3,4 5,6"} {
		if _, err := splinePath(bad, flip); err == nil {
			t.Errorf("splinePath(%q) succeeded, want error", bad)
		}
	}
}
