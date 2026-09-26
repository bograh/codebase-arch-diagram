package diff

import (
	"reflect"
	"testing"

	"codebase/arch/internal/model"
)

func clusterGraph(extra ...*model.Node) *model.Graph {
	nodes := []*model.Node{
		local(""), local("cmd/app"), local("internal/api"), local("internal/db"),
		{ID: "fmt", Kind: model.Std},
	}
	return graph(append(nodes, extra...))
}

func clusterByPath(d *DiffGraph) map[string]*Cluster {
	m := map[string]*Cluster{}
	for _, c := range d.Clusters {
		m[c.Path] = c
	}
	return m
}

func TestClustersFollowDirectories(t *testing.T) {
	g := clusterGraph(local("internal/api/v2"))
	d := Compute(g, g)
	cs := clusterByPath(d)

	if len(cs) != 3 {
		t.Fatalf("clusters = %v, want internal, internal/api, @deps", keys(cs))
	}
	internal := cs["internal"]
	if internal == nil || internal.Parent != "" || internal.Label != "internal" {
		t.Fatalf("internal cluster = %+v", internal)
	}
	if !reflect.DeepEqual(internal.Nodes, []string{pkg("internal/db")}) {
		t.Errorf("internal nodes = %v", internal.Nodes)
	}
	if !reflect.DeepEqual(internal.Children, []string{"internal/api"}) {
		t.Errorf("internal children = %v", internal.Children)
	}
	api := cs["internal/api"]
	if api == nil || api.Parent != "internal" || api.Label != "api" {
		t.Fatalf("api cluster = %+v", api)
	}
	if want := []string{pkg("internal/api"), pkg("internal/api/v2")}; !reflect.DeepEqual(api.Nodes, want) {
		t.Errorf("api nodes = %v, want %v", api.Nodes, want)
	}
	if deps := cs[DepsCluster]; deps == nil || !reflect.DeepEqual(deps.Nodes, []string{"fmt"}) {
		t.Errorf("deps cluster = %+v", deps)
	}
	// "cmd" holds a single package, so it is dissolved.
	if want := []string{pkg(""), pkg("cmd/app")}; !reflect.DeepEqual(d.RootNodes, want) {
		t.Errorf("root nodes = %v, want %v", d.RootNodes, want)
	}
	for _, c := range d.Clusters {
		if c.Touched {
			t.Errorf("cluster %s touched in an unchanged graph", c.Path)
		}
	}
}

func TestClustersSortParentsFirst(t *testing.T) {
	g := clusterGraph(local("internal/api/v2"))
	d := Compute(g, g)
	seen := map[string]bool{}
	for _, c := range d.Clusters {
		if c.Parent != "" && !seen[c.Parent] {
			t.Errorf("cluster %s listed before its parent %s", c.Path, c.Parent)
		}
		seen[c.Path] = true
	}
}

func TestTouchedPropagatesUp(t *testing.T) {
	base := clusterGraph()
	cur := clusterGraph(local("internal/api/v2"))
	cs := clusterByPath(Compute(base, cur))

	if !cs["internal/api"].Touched || !cs["internal"].Touched {
		t.Error("adding internal/api/v2 should touch internal/api and internal")
	}
	if cs[DepsCluster].Touched {
		t.Error("deps cluster should not be touched")
	}
}

func TestTouchedByEdge(t *testing.T) {
	base := clusterGraph(local("internal/api/v2"))
	cur := clusterGraph(local("internal/api/v2"))
	cur.Edges = []model.Edge{edge("internal/db", "")}
	cs := clusterByPath(Compute(base, cur))

	if !cs["internal"].Touched {
		t.Error("new edge from internal/db should touch internal")
	}
	if cs["internal/api"].Touched {
		t.Error("internal/api should not be touched")
	}
}

func keys(m map[string]*Cluster) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
