// Package model defines the package-level architecture graph shared by the
// extract, diff and layout stages.
package model

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
)

// NodeKind says where a package comes from.
type NodeKind string

const (
	Local    NodeKind = "local"    // package in the main module
	Std      NodeKind = "std"      // standard library
	External NodeKind = "external" // third-party module
)

// Node is one package, or one collapsed third-party module.
type Node struct {
	ID      string   `json:"id"` // import path, or module path for collapsed deps
	Kind    NodeKind `json:"kind"`
	Dir     string   `json:"dir,omitempty"`     // module-relative dir using "/"; "" for the module root (local only)
	Module  string   `json:"module,omitempty"`  // owning module path (external only)
	Files   []string `json:"files,omitempty"`   // sorted .go file base names (local only)
	Exports []string `json:"exports,omitempty"` // sorted exported identifiers (local only)
	Errors  []string `json:"errors,omitempty"`  // load and type errors
}

// Label is the short name shown in the diagram: the module-relative
// directory for local packages, the full path for everything else.
func (n *Node) Label(module string) string {
	if n.Kind != Local {
		return n.ID
	}
	if n.Dir == "" {
		return path.Base(module)
	}
	return n.Dir
}

// Edge is an import from one package to another.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// CompareEdges orders edges by From, then To.
func CompareEdges(a, b Edge) int {
	if c := strings.Compare(a.From, b.From); c != 0 {
		return c
	}
	return strings.Compare(a.To, b.To)
}

// Graph is the package import graph of one module.
type Graph struct {
	Module string           `json:"module"`
	Nodes  map[string]*Node `json:"nodes"`
	Edges  []Edge           `json:"edges"`
}

// NewGraph returns an empty graph for module.
func NewGraph(module string) *Graph {
	return &Graph{Module: module, Nodes: map[string]*Node{}}
}

// Normalize sorts and deduplicates edges and per-node lists, so equal graphs
// have identical representations.
func (g *Graph) Normalize() {
	for _, n := range g.Nodes {
		n.Files = sortedUnique(n.Files)
		n.Exports = sortedUnique(n.Exports)
		n.Errors = sortedUnique(n.Errors)
	}
	slices.SortFunc(g.Edges, CompareEdges)
	g.Edges = slices.Compact(g.Edges)
}

// SortedIDs returns the node IDs in lexical order.
func (g *Graph) SortedIDs() []string {
	ids := make([]string, 0, len(g.Nodes))
	for id := range g.Nodes {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// Write encodes g as JSON.
func (g *Graph) Write(w io.Writer) error {
	return json.NewEncoder(w).Encode(g)
}

// Read decodes a graph written by Write and checks that every edge refers
// to a known node.
func Read(r io.Reader) (*Graph, error) {
	var g Graph
	if err := json.NewDecoder(r).Decode(&g); err != nil {
		return nil, fmt.Errorf("decode graph: %w", err)
	}
	if g.Module == "" {
		return nil, fmt.Errorf("decode graph: missing module")
	}
	if g.Nodes == nil {
		g.Nodes = map[string]*Node{}
	}
	for _, e := range g.Edges {
		if g.Nodes[e.From] == nil || g.Nodes[e.To] == nil {
			return nil, fmt.Errorf("decode graph: edge %s -> %s references an unknown node", e.From, e.To)
		}
	}
	return &g, nil
}

func sortedUnique(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	s = slices.Clone(s)
	slices.Sort(s)
	return slices.Compact(s)
}
