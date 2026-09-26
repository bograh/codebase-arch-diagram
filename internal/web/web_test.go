package web

import (
	"bytes"
	"context"
	"io/fs"
	"reflect"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"codebase/arch/internal/diff"
	"codebase/arch/internal/layout"
	"codebase/arch/internal/model"
)

const mod = "example.com/m"

func node(dir string, errs ...string) *model.Node {
	id := mod
	if dir != "" {
		id += "/" + dir
	}
	return &model.Node{ID: id, Kind: model.Local, Dir: dir, Errors: errs}
}

// sampleView: internal/api is new (with an error), main now imports api
// instead of db, api imports db.
func sampleView() View {
	base := model.NewGraph(mod)
	for _, n := range []*model.Node{node(""), node("internal/db")} {
		base.Nodes[n.ID] = n
	}
	base.Edges = []model.Edge{{From: mod, To: mod + "/internal/db"}}

	cur := model.NewGraph(mod)
	for _, n := range []*model.Node{node(""), node("internal/db"), node("internal/api", "api.go:1:1: boom")} {
		cur.Nodes[n.ID] = n
	}
	cur.Edges = []model.Edge{
		{From: mod, To: mod + "/internal/api"},
		{From: mod + "/internal/api", To: mod + "/internal/db"},
	}
	d := diff.Compute(base, cur)

	l := &layout.Layout{Width: 400, Height: 300}
	x := 10.0
	for _, id := range []string{mod, mod + "/internal/api", mod + "/internal/db"} {
		n := d.Nodes[id]
		l.Nodes = append(l.Nodes, layout.NodeBox{ID: id, Label: n.Label(mod), X: x, Y: 10, W: 100, H: 36,
			Status: n.Status, Kind: n.Kind, HasErrors: len(n.Errors) > 0})
		x += 120
	}
	for _, e := range d.Edges {
		l.Edges = append(l.Edges, layout.EdgePath{From: e.From, To: e.To, D: "M0,0 C1,1 2,2 3,3", Status: e.Status})
	}
	l.Clusters = []layout.ClusterBox{{Path: "internal", Label: "internal", W: 300, H: 100, Touched: true}}

	return View{Module: mod, BaseRef: "main", BaseSHA: "0123456789abcdef", Diff: d, Layout: l}
}

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func assertContains(t *testing.T, html string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(html, w) {
			t.Errorf("output does not contain %q", w)
		}
	}
}

func TestDiagramMarksStatuses(t *testing.T) {
	html := render(t, Diagram(sampleView().Layout, false))
	assertContains(t, html,
		`<g id="n-example.com/m/internal/api" class="node st-added kind-local"`,
		`hx-get="/node/example.com/m/internal/api"`,
		`id="e-example.com/m|example.com/m/internal/db" class="edge st-removed"`,
		`marker-end="url(#arrow-removed)"`,
		`class="cluster touched"`,
		`⚠`,
	)
	if strings.Contains(html, "<style>") {
		t.Error("in-app diagram should not inline styles")
	}
}

func TestStandaloneDiagramIsSelfContained(t *testing.T) {
	html := render(t, Diagram(sampleView().Layout, true))
	assertContains(t, html, `xmlns="http://www.w3.org/2000/svg"`, `<style>`, `height="300.0"`, `width="400.0"`)
}

func TestGroups(t *testing.T) {
	got := map[string][]string{}
	var order []string
	for _, g := range sampleView().Groups() {
		order = append(order, g.Title)
		for _, it := range g.Items {
			got[g.Title] = append(got[g.Title], it.Text+" @ "+it.Target)
		}
	}
	if want := []string{"Added packages", "Added imports", "Removed imports"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("groups = %v, want %v", order, want)
	}
	want := map[string][]string{
		"Added packages": {"internal/api @ n-example.com/m/internal/api"},
		"Added imports": {
			"m → internal/api @ e-example.com/m|example.com/m/internal/api",
			"internal/api → internal/db @ e-example.com/m/internal/api|example.com/m/internal/db",
		},
		"Removed imports": {"m → internal/db @ e-example.com/m|example.com/m/internal/db"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("items = %v, want %v", got, want)
	}
}

func TestDetails(t *testing.T) {
	v := sampleView()
	d, ok := v.Details(mod + "/internal/api")
	if !ok {
		t.Fatal("details not found")
	}
	if d.Label != "internal/api" {
		t.Errorf("label = %q", d.Label)
	}
	if want := []DetailLink{{ID: mod + "/internal/db", Label: "internal/db", Status: diff.Added}}; !reflect.DeepEqual(d.Imports, want) {
		t.Errorf("imports = %v, want %v", d.Imports, want)
	}
	if want := []DetailLink{{ID: mod, Label: "m", Status: diff.Added}}; !reflect.DeepEqual(d.Importers, want) {
		t.Errorf("importers = %v, want %v", d.Importers, want)
	}
	assertContains(t, render(t, Details(d)), "internal/api", "api.go:1:1: boom", "Imported by")

	if _, ok := v.Details("nope"); ok {
		t.Error("details found for unknown id")
	}
	if _, ok := (View{}).Details(mod); ok {
		t.Error("details found in an empty view")
	}
}

func TestPageHasLiveRegions(t *testing.T) {
	assertContains(t, render(t, Page(sampleView())),
		`<!doctype html>`,
		`sse-connect="/events"`,
		`sse-swap="diagram"`,
		`sse-swap="summary"`,
		`hx-swap="morph:innerHTML"`,
		`src="/static/htmx.min.js"`,
		`id="details"`,
	)
}

func TestSummaryShowsBase(t *testing.T) {
	assertContains(t, render(t, Summary(sampleView())), "Compared with", "<code>main</code>", "0123456789<")
	assertContains(t, render(t, Summary(View{})), "No base")
}

func TestCanvasBannerAndEmptyState(t *testing.T) {
	assertContains(t, render(t, Canvas(View{Banner: "Stale: boom"})), "Stale: boom", "No packages to show yet.")
}

func TestStaticAssetsAreEmbedded(t *testing.T) {
	for _, name := range []string{"htmx.min.js", "sse.js", "idiomorph-ext.min.js", "app.js", "app.css", "diagram.css"} {
		if fi, err := fs.Stat(Static, "static/"+name); err != nil || fi.Size() == 0 {
			t.Errorf("static/%s missing or empty: %v", name, err)
		}
	}
}
