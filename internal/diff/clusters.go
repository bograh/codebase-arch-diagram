package diff

import (
	"cmp"
	"slices"
	"strings"

	"codebase/arch/internal/model"
)

// DepsCluster is the path of the cluster holding standard-library and
// third-party nodes.
const DepsCluster = "@deps"

// Cluster is a directory box in the diagram.
type Cluster struct {
	Path     string   // module-relative directory, or DepsCluster
	Label    string   // Path relative to the parent cluster
	Parent   string   // parent cluster path; "" at top level
	Nodes    []string // IDs of nodes directly in this cluster, sorted
	Children []string // paths of direct child clusters, sorted
	Touched  bool     // something inside differs between base and current
}

// buildClusters groups local nodes by directory. Every directory above a
// package is a candidate cluster; candidates with fewer than two direct
// members (packages or subclusters) are dissolved into their parent so that
// single-package directories don't add empty nesting.
func buildClusters(d *DiffGraph) ([]*Cluster, []string) {
	exists := map[string]bool{}
	for _, n := range d.Nodes {
		if n.Kind == model.Local {
			for p := parentDir(n.Dir); p != ""; p = parentDir(p) {
				exists[p] = true
			}
		}
	}
	// nearest returns the closest existing proper ancestor of p ("" = top level).
	nearest := func(p string) string {
		for q := parentDir(p); q != ""; q = parentDir(q) {
			if exists[q] {
				return q
			}
		}
		return ""
	}

	members := map[string][]string{} // cluster path ("" = top level) -> node IDs
	var deps []string
	for id, n := range d.Nodes {
		switch {
		case n.Kind != model.Local:
			deps = append(deps, id)
		case exists[n.Dir]:
			members[n.Dir] = append(members[n.Dir], id)
		default:
			up := nearest(n.Dir)
			members[up] = append(members[up], id)
		}
	}

	for _, p := range deepestFirst(exists) {
		children := 0
		for q := range exists {
			if nearest(q) == p {
				children++
			}
		}
		if len(members[p])+children < 2 {
			up := nearest(p)
			members[up] = append(members[up], members[p]...)
			delete(members, p)
			delete(exists, p)
		}
	}

	touchedNode := map[string]bool{}
	for id, n := range d.Nodes {
		if n.Status != Same {
			touchedNode[id] = true
		}
	}
	for _, e := range d.Edges {
		if e.Status != Same {
			touchedNode[e.From] = true
		}
	}

	byPath := map[string]*Cluster{}
	var out []*Cluster
	for p := range exists {
		parent := nearest(p)
		label := p
		if parent != "" {
			label = strings.TrimPrefix(p, parent+"/")
		}
		c := &Cluster{Path: p, Label: label, Parent: parent, Nodes: sorted(members[p])}
		byPath[p] = c
		out = append(out, c)
	}
	for _, c := range out {
		if c.Parent != "" {
			byPath[c.Parent].Children = append(byPath[c.Parent].Children, c.Path)
		}
	}
	for _, p := range deepestFirst(exists) {
		c := byPath[p]
		slices.Sort(c.Children)
		c.Touched = slices.ContainsFunc(c.Nodes, func(id string) bool { return touchedNode[id] }) ||
			slices.ContainsFunc(c.Children, func(ch string) bool { return byPath[ch].Touched })
	}
	slices.SortFunc(out, func(a, b *Cluster) int { return strings.Compare(a.Path, b.Path) })

	if len(deps) > 0 {
		dc := &Cluster{Path: DepsCluster, Label: "dependencies", Nodes: sorted(deps)}
		dc.Touched = slices.ContainsFunc(dc.Nodes, func(id string) bool { return touchedNode[id] })
		out = append(out, dc)
	}
	return out, sorted(members[""])
}

func parentDir(p string) string {
	i := strings.LastIndex(p, "/")
	if i < 0 {
		return ""
	}
	return p[:i]
}

func depth(p string) int { return strings.Count(p, "/") + 1 }

func deepestFirst(set map[string]bool) []string {
	paths := make([]string, 0, len(set))
	for p := range set {
		paths = append(paths, p)
	}
	slices.SortFunc(paths, func(a, b string) int {
		if c := cmp.Compare(depth(b), depth(a)); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})
	return paths
}

func sorted(s []string) []string {
	s = slices.Clone(s)
	slices.Sort(s)
	return s
}
