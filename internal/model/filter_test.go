package model

import (
	"reflect"
	"testing"
)

func depsGraph() *Graph {
	g := NewGraph("example.com/m")
	for _, n := range []*Node{
		{ID: "example.com/m", Kind: Local},
		{ID: "fmt", Kind: Std},
		{ID: "github.com/x/y/a", Kind: External, Module: "github.com/x/y"},
		{ID: "github.com/x/y/b", Kind: External, Module: "github.com/x/y"},
	} {
		g.Nodes[n.ID] = n
	}
	g.Edges = []Edge{
		{From: "example.com/m", To: "fmt"},
		{From: "example.com/m", To: "github.com/x/y/a"},
		{From: "example.com/m", To: "github.com/x/y/b"},
		{From: "github.com/x/y/a", To: "github.com/x/y/b"},
	}
	return g
}

func TestFilter(t *testing.T) {
	for _, tc := range []struct {
		name      string
		opts      FilterOptions
		wantNodes []string
		wantEdges []Edge
	}{
		{
			name:      "zero options keep local only",
			wantNodes: []string{"example.com/m"},
		},
		{
			name:      "std shown",
			opts:      FilterOptions{ShowStd: true, Deps: DepsHidden},
			wantNodes: []string{"example.com/m", "fmt"},
			wantEdges: []Edge{{From: "example.com/m", To: "fmt"}},
		},
		{
			name:      "deps collapsed to modules",
			opts:      FilterOptions{Deps: DepsCollapsed},
			wantNodes: []string{"example.com/m", "github.com/x/y"},
			wantEdges: []Edge{{From: "example.com/m", To: "github.com/x/y"}},
		},
		{
			name:      "deps in full",
			opts:      FilterOptions{Deps: DepsFull},
			wantNodes: []string{"example.com/m", "github.com/x/y/a", "github.com/x/y/b"},
			wantEdges: []Edge{
				{From: "example.com/m", To: "github.com/x/y/a"},
				{From: "example.com/m", To: "github.com/x/y/b"},
				{From: "github.com/x/y/a", To: "github.com/x/y/b"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := depsGraph()
			got := Filter(in, tc.opts)
			if ids := got.SortedIDs(); !reflect.DeepEqual(ids, tc.wantNodes) {
				t.Errorf("nodes = %v, want %v", ids, tc.wantNodes)
			}
			if !reflect.DeepEqual(got.Edges, tc.wantEdges) {
				t.Errorf("edges = %v, want %v", got.Edges, tc.wantEdges)
			}
			if len(in.Nodes) != 4 || len(in.Edges) != 4 {
				t.Error("Filter modified its input")
			}
		})
	}
}

func TestParseDepsMode(t *testing.T) {
	for _, s := range []string{"collapsed", "hidden", "full"} {
		if m, err := ParseDepsMode(s); err != nil || string(m) != s {
			t.Errorf("ParseDepsMode(%q) = %q, %v", s, m, err)
		}
	}
	if _, err := ParseDepsMode("everything"); err == nil {
		t.Error("ParseDepsMode(everything) succeeded, want error")
	}
}
