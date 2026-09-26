package model

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func sampleGraph() *Graph {
	g := NewGraph("example.com/m")
	g.Nodes["example.com/m"] = &Node{ID: "example.com/m", Kind: Local, Files: []string{"main.go"}}
	g.Nodes["example.com/m/internal/db"] = &Node{
		ID: "example.com/m/internal/db", Kind: Local, Dir: "internal/db",
		Files: []string{"b.go", "a.go", "a.go"}, Exports: []string{"Open", "Conn"},
	}
	g.Nodes["fmt"] = &Node{ID: "fmt", Kind: Std}
	g.Edges = []Edge{
		{From: "example.com/m", To: "fmt"},
		{From: "example.com/m", To: "example.com/m/internal/db"},
		{From: "example.com/m", To: "fmt"},
	}
	return g
}

func TestNormalizeSortsAndDeduplicates(t *testing.T) {
	g := sampleGraph()
	g.Normalize()

	wantEdges := []Edge{
		{From: "example.com/m", To: "example.com/m/internal/db"},
		{From: "example.com/m", To: "fmt"},
	}
	if !reflect.DeepEqual(g.Edges, wantEdges) {
		t.Errorf("edges = %v, want %v", g.Edges, wantEdges)
	}
	db := g.Nodes["example.com/m/internal/db"]
	if want := []string{"a.go", "b.go"}; !reflect.DeepEqual(db.Files, want) {
		t.Errorf("files = %v, want %v", db.Files, want)
	}
	if want := []string{"Conn", "Open"}; !reflect.DeepEqual(db.Exports, want) {
		t.Errorf("exports = %v, want %v", db.Exports, want)
	}
}

func TestWriteReadRoundTrip(t *testing.T) {
	g := sampleGraph()
	g.Normalize()

	var buf bytes.Buffer
	if err := g.Write(&buf); err != nil {
		t.Fatal(err)
	}
	got, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, g) {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", got, g)
	}
}

func TestReadRejectsBadInput(t *testing.T) {
	for name, input := range map[string]string{
		"garbage":        "{not json",
		"missing module": `{"nodes":{}}`,
		"dangling edge":  `{"module":"m","nodes":{"a":{"id":"a","kind":"local"}},"edges":[{"from":"a","to":"b"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Read(strings.NewReader(input)); err == nil {
				t.Error("Read succeeded, want error")
			}
		})
	}
}

func TestLabel(t *testing.T) {
	for _, tc := range []struct {
		node Node
		want string
	}{
		{Node{ID: "example.com/m", Kind: Local}, "m"},
		{Node{ID: "example.com/m/internal/db", Kind: Local, Dir: "internal/db"}, "internal/db"},
		{Node{ID: "fmt", Kind: Std}, "fmt"},
		{Node{ID: "github.com/x/y", Kind: External}, "github.com/x/y"},
	} {
		if got := tc.node.Label("example.com/m"); got != tc.want {
			t.Errorf("Label(%s) = %q, want %q", tc.node.ID, got, tc.want)
		}
	}
}
