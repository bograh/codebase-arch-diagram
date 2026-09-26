package diff

import (
	"reflect"
	"testing"

	"codebase/arch/internal/model"
)

const mod = "example.com/m"

func pkg(dir string) string {
	if dir == "" {
		return mod
	}
	return mod + "/" + dir
}

func local(dir string, exports ...string) *model.Node {
	return &model.Node{ID: pkg(dir), Kind: model.Local, Dir: dir, Files: []string{"f.go"}, Exports: exports}
}

func graph(nodes []*model.Node, edges ...model.Edge) *model.Graph {
	g := model.NewGraph(mod)
	for _, n := range nodes {
		g.Nodes[n.ID] = n
	}
	g.Edges = edges
	g.Normalize()
	return g
}

func edge(from, to string) model.Edge { return model.Edge{From: pkg(from), To: pkg(to)} }

func TestComputeStatuses(t *testing.T) {
	base := graph([]*model.Node{local("a", "X"), local("b"), local("c")},
		edge("a", "b"), edge("a", "c"))
	cur := graph([]*model.Node{local("a", "X", "Y"), local("b"), local("d")},
		edge("a", "b"), edge("a", "d"))

	d := Compute(base, cur)

	wantNodes := map[string]Status{pkg("a"): Changed, pkg("b"): Same, pkg("c"): Removed, pkg("d"): Added}
	for id, want := range wantNodes {
		if got := d.Nodes[id].Status; got != want {
			t.Errorf("node %s status = %s, want %s", id, got, want)
		}
	}
	if len(d.Nodes) != len(wantNodes) {
		t.Errorf("got %d nodes, want %d", len(d.Nodes), len(wantNodes))
	}
	if got := d.Nodes[pkg("a")].AddedExports; !reflect.DeepEqual(got, []string{"Y"}) {
		t.Errorf("a added exports = %v, want [Y]", got)
	}
	if d.Nodes[pkg("c")].Node != base.Nodes[pkg("c")] {
		t.Error("removed node should carry the base version")
	}

	wantEdges := []DiffEdge{
		{Edge: edge("a", "b"), Status: Same},
		{Edge: edge("a", "c"), Status: Removed},
		{Edge: edge("a", "d"), Status: Added},
	}
	if !reflect.DeepEqual(d.Edges, wantEdges) {
		t.Errorf("edges = %v, want %v", d.Edges, wantEdges)
	}

	want := Summary{AddedNodes: 1, RemovedNodes: 1, ChangedNodes: 1, AddedEdges: 1, RemovedEdges: 1}
	if got := d.Summary(); got != want {
		t.Errorf("summary = %+v, want %+v", got, want)
	}
}

func TestInternalEditsAreSame(t *testing.T) {
	base := graph([]*model.Node{local("a", "X")})
	cur := graph([]*model.Node{local("a", "X")})
	d := Compute(base, cur)
	if got := d.Nodes[pkg("a")].Status; got != Same {
		t.Errorf("status = %s, want same", got)
	}
	if !d.Summary().Empty() {
		t.Errorf("summary = %+v, want empty", d.Summary())
	}
}

func TestFileChangesMakeNodeChanged(t *testing.T) {
	base := graph([]*model.Node{local("a")})
	moved := local("a")
	moved.Files = []string{"f.go", "g.go"}
	d := Compute(base, graph([]*model.Node{moved}))

	n := d.Nodes[pkg("a")]
	if n.Status != Changed || !reflect.DeepEqual(n.AddedFiles, []string{"g.go"}) {
		t.Errorf("node = %+v, want changed with added file g.go", n)
	}
}

func TestQueries(t *testing.T) {
	base := graph([]*model.Node{local("a"), local("b"), local("c")}, edge("a", "c"))
	cur := graph([]*model.Node{local("a"), local("b"), local("c")}, edge("a", "b"), edge("c", "b"))
	d := Compute(base, cur)

	if got := d.EdgesWith(Added); len(got) != 2 {
		t.Errorf("added edges = %v, want 2", got)
	}
	if got := d.Imports(pkg("a")); len(got) != 2 {
		t.Errorf("imports of a = %v, want a->b and a->c", got)
	}
	if got := d.Importers(pkg("b")); len(got) != 2 {
		t.Errorf("importers of b = %v, want a and c", got)
	}
	if got := d.NodesWith(Same); len(got) != 3 || got[0].ID != pkg("a") {
		t.Errorf("same nodes = %v, want a, b, c in order", got)
	}
}
