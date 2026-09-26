package diff

import (
	"slices"
	"strings"
)

// Summary counts what changed.
type Summary struct {
	AddedNodes, RemovedNodes, ChangedNodes int
	AddedEdges, RemovedEdges               int
}

// Empty reports whether nothing changed.
func (s Summary) Empty() bool { return s == Summary{} }

// Summary counts the non-Same nodes and edges.
func (d *DiffGraph) Summary() Summary {
	var s Summary
	for _, n := range d.Nodes {
		switch n.Status {
		case Added:
			s.AddedNodes++
		case Removed:
			s.RemovedNodes++
		case Changed:
			s.ChangedNodes++
		}
	}
	for _, e := range d.Edges {
		switch e.Status {
		case Added:
			s.AddedEdges++
		case Removed:
			s.RemovedEdges++
		}
	}
	return s
}

// NodesWith returns the nodes with status st, sorted by ID.
func (d *DiffGraph) NodesWith(st Status) []*DiffNode {
	var out []*DiffNode
	for _, n := range d.Nodes {
		if n.Status == st {
			out = append(out, n)
		}
	}
	slices.SortFunc(out, func(a, b *DiffNode) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// EdgesWith returns the edges with status st, in edge order.
func (d *DiffGraph) EdgesWith(st Status) []DiffEdge {
	return d.edgesWhere(func(e DiffEdge) bool { return e.Status == st })
}

// Imports returns the edges leaving id.
func (d *DiffGraph) Imports(id string) []DiffEdge {
	return d.edgesWhere(func(e DiffEdge) bool { return e.From == id })
}

// Importers returns the edges arriving at id.
func (d *DiffGraph) Importers(id string) []DiffEdge {
	return d.edgesWhere(func(e DiffEdge) bool { return e.To == id })
}

func (d *DiffGraph) edgesWhere(keep func(DiffEdge) bool) []DiffEdge {
	var out []DiffEdge
	for _, e := range d.Edges {
		if keep(e) {
			out = append(out, e)
		}
	}
	return out
}
