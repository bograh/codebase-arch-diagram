// Package diff compares two architecture graphs and groups the result into
// directory clusters.
package diff

import (
	"slices"

	"codebase/arch/internal/model"
)

// Status says how a node or edge differs between base and current.
type Status string

const (
	Same    Status = "same"
	Added   Status = "added"   // only in current
	Removed Status = "removed" // only in base
	Changed Status = "changed" // in both; exports or files differ (nodes only)
)

// DiffNode is a node of the union graph and how it differs.
type DiffNode struct {
	*model.Node    // current version, or the base version when Removed
	Status         Status
	AddedExports   []string
	RemovedExports []string
	AddedFiles     []string
	RemovedFiles   []string
}

// DiffEdge is an edge of the union graph and how it differs.
type DiffEdge struct {
	model.Edge
	Status Status
}

// DiffGraph is the union of a base and a current graph.
type DiffGraph struct {
	Module    string
	Nodes     map[string]*DiffNode
	Edges     []DiffEdge // sorted by From, then To
	Clusters  []*Cluster // sorted by Path so parents precede children; DepsCluster last
	RootNodes []string   // sorted IDs of nodes in no cluster
}

// Compute returns the union of base and current with every node and edge
// tagged by status. Neither input is modified. Internal-only edits (same
// exports and files) leave a node Same.
func Compute(base, current *model.Graph) *DiffGraph {
	d := &DiffGraph{Module: current.Module, Nodes: map[string]*DiffNode{}}
	for id, cur := range current.Nodes {
		old, ok := base.Nodes[id]
		if !ok {
			d.Nodes[id] = &DiffNode{Node: cur, Status: Added}
			continue
		}
		n := &DiffNode{Node: cur, Status: Same}
		n.AddedExports, n.RemovedExports = setDelta(old.Exports, cur.Exports)
		n.AddedFiles, n.RemovedFiles = setDelta(old.Files, cur.Files)
		if len(n.AddedExports)+len(n.RemovedExports)+len(n.AddedFiles)+len(n.RemovedFiles) > 0 {
			n.Status = Changed
		}
		d.Nodes[id] = n
	}
	for id, old := range base.Nodes {
		if _, ok := current.Nodes[id]; !ok {
			d.Nodes[id] = &DiffNode{Node: old, Status: Removed}
		}
	}

	inBase := map[model.Edge]bool{}
	for _, e := range base.Edges {
		inBase[e] = true
	}
	inCur := map[model.Edge]bool{}
	for _, e := range current.Edges {
		inCur[e] = true
		status := Added
		if inBase[e] {
			status = Same
		}
		d.Edges = append(d.Edges, DiffEdge{Edge: e, Status: status})
	}
	for _, e := range base.Edges {
		if !inCur[e] {
			d.Edges = append(d.Edges, DiffEdge{Edge: e, Status: Removed})
		}
	}
	slices.SortFunc(d.Edges, func(a, b DiffEdge) int { return model.CompareEdges(a.Edge, b.Edge) })
	d.Edges = slices.CompactFunc(d.Edges, func(a, b DiffEdge) bool { return a.Edge == b.Edge })

	d.Clusters, d.RootNodes = buildClusters(d)
	return d
}

// setDelta returns the sorted elements only in b (added) and only in a (removed).
func setDelta(a, b []string) (added, removed []string) {
	inA, inB := map[string]bool{}, map[string]bool{}
	for _, s := range a {
		inA[s] = true
	}
	for _, s := range b {
		inB[s] = true
	}
	for _, s := range b {
		if !inA[s] {
			added = append(added, s)
		}
	}
	for _, s := range a {
		if !inB[s] {
			removed = append(removed, s)
		}
	}
	slices.Sort(added)
	slices.Sort(removed)
	return added, removed
}
