package model

import "fmt"

// DepsMode controls how third-party packages appear in the diagram.
type DepsMode string

const (
	DepsCollapsed DepsMode = "collapsed" // one node per module
	DepsHidden    DepsMode = "hidden"    // omitted
	DepsFull      DepsMode = "full"      // one node per package
)

// ParseDepsMode validates a --deps flag value.
func ParseDepsMode(s string) (DepsMode, error) {
	switch m := DepsMode(s); m {
	case DepsCollapsed, DepsHidden, DepsFull:
		return m, nil
	}
	return "", fmt.Errorf("invalid deps mode %q (want collapsed, hidden or full)", s)
}

// FilterOptions selects which non-local packages to show. The zero value
// shows local packages only.
type FilterOptions struct {
	ShowStd bool
	Deps    DepsMode
}

// Filter returns a copy of g with standard-library and third-party packages
// kept, hidden or collapsed according to opts. g is not modified.
func Filter(g *Graph, opts FilterOptions) *Graph {
	out := NewGraph(g.Module)
	newID := map[string]string{} // original ID -> ID in out; absent means dropped
	for id, n := range g.Nodes {
		switch {
		case n.Kind == Local,
			n.Kind == Std && opts.ShowStd,
			n.Kind == External && opts.Deps == DepsFull:
			c := *n
			out.Nodes[id] = &c
			newID[id] = id
		case n.Kind == External && opts.Deps == DepsCollapsed:
			mod := n.Module
			if mod == "" {
				mod = id
			}
			newID[id] = mod
			if out.Nodes[mod] == nil {
				out.Nodes[mod] = &Node{ID: mod, Kind: External, Module: mod}
			}
		}
	}
	for _, e := range g.Edges {
		from, okFrom := newID[e.From]
		to, okTo := newID[e.To]
		if okFrom && okTo && from != to {
			out.Edges = append(out.Edges, Edge{From: from, To: to})
		}
	}
	out.Normalize()
	return out
}
