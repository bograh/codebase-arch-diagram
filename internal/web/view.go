// Package web renders the archdiag UI with templ.
package web

//go:generate go tool templ generate

import (
	"strconv"

	"github.com/a-h/templ"

	"codebase/arch/internal/diff"
	"codebase/arch/internal/layout"
)

// View is everything the page shows at one moment.
type View struct {
	Module  string
	BaseRef string
	BaseSHA string // "" when there is no base
	Banner  string // warning shown over the diagram; "" for none
	Diff    *diff.DiffGraph
	Layout  *layout.Layout
}

// Empty reports whether there is nothing to draw.
func (v View) Empty() bool { return v.Layout == nil || len(v.Layout.Nodes) == 0 }

// SummaryItem is one clickable entry in the change summary.
type SummaryItem struct {
	Target string // DOM id of the diagram element
	Text   string
}

// SummaryGroup is one heading of the change summary.
type SummaryGroup struct {
	Title  string
	Status diff.Status
	Items  []SummaryItem
}

// Groups lists what changed, for the sidebar. Empty groups are omitted.
func (v View) Groups() []SummaryGroup {
	if v.Diff == nil {
		return nil
	}
	var out []SummaryGroup
	add := func(title string, st diff.Status, items []SummaryItem) {
		if len(items) > 0 {
			out = append(out, SummaryGroup{Title: title, Status: st, Items: items})
		}
	}
	nodes := func(st diff.Status) []SummaryItem {
		var items []SummaryItem
		for _, n := range v.Diff.NodesWith(st) {
			items = append(items, SummaryItem{Target: NodeDOMID(n.ID), Text: v.label(n.ID)})
		}
		return items
	}
	edges := func(st diff.Status) []SummaryItem {
		var items []SummaryItem
		for _, e := range v.Diff.EdgesWith(st) {
			items = append(items, SummaryItem{Target: EdgeDOMID(e.From, e.To), Text: v.label(e.From) + " → " + v.label(e.To)})
		}
		return items
	}
	add("Added packages", diff.Added, nodes(diff.Added))
	add("Removed packages", diff.Removed, nodes(diff.Removed))
	add("Changed packages", diff.Changed, nodes(diff.Changed))
	add("Added imports", diff.Added, edges(diff.Added))
	add("Removed imports", diff.Removed, edges(diff.Removed))
	return out
}

// DetailLink is a neighbouring package in the details panel.
type DetailLink struct {
	ID, Label string
	Status    diff.Status // status of the connecting edge
}

// NodeDetails is the content of the details panel for one package.
type NodeDetails struct {
	Node      *diff.DiffNode
	Label     string
	Imports   []DetailLink
	Importers []DetailLink
}

// Details describes the node with the given ID, if it exists.
func (v View) Details(id string) (NodeDetails, bool) {
	if v.Diff == nil || v.Diff.Nodes[id] == nil {
		return NodeDetails{}, false
	}
	n := v.Diff.Nodes[id]
	d := NodeDetails{Node: n, Label: v.label(id)}
	for _, e := range v.Diff.Imports(id) {
		d.Imports = append(d.Imports, DetailLink{ID: e.To, Label: v.label(e.To), Status: e.Status})
	}
	for _, e := range v.Diff.Importers(id) {
		d.Importers = append(d.Importers, DetailLink{ID: e.From, Label: v.label(e.From), Status: e.Status})
	}
	return d, true
}

func (v View) label(id string) string {
	if n := v.Diff.Nodes[id]; n != nil {
		return n.Label(v.Diff.Module)
	}
	return id
}

// NodeDOMID is the DOM id of a node's <g> element.
func NodeDOMID(id string) string { return "n-" + id }

// EdgeDOMID is the DOM id of an edge's <path>. "|" cannot occur in import paths.
func EdgeDOMID(from, to string) string { return "e-" + from + "|" + to }

// ClusterDOMID is the DOM id of a cluster's <g> element.
func ClusterDOMID(path string) string { return "c-" + path }

func num(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }

func viewBox(l *layout.Layout) string { return "0 0 " + num(l.Width) + " " + num(l.Height) }

func statusClass(s diff.Status) string { return "st-" + string(s) }

func shortSHA(s string) string {
	if len(s) > 10 {
		return s[:10]
	}
	return s
}

func sizeAttrs(l *layout.Layout, standalone bool) templ.Attributes {
	if !standalone {
		return templ.Attributes{}
	}
	return templ.Attributes{"width": num(l.Width), "height": num(l.Height)}
}
