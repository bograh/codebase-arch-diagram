package server

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codebase/arch/internal/extract"
	"codebase/arch/internal/layout"
	"codebase/arch/internal/model"
	"codebase/arch/internal/watch"
)

func newEngine(t *testing.T) *layout.Engine {
	t.Helper()
	e, err := layout.New(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	return e
}

func handGraph() *model.Graph {
	g := model.NewGraph("example.com/m")
	g.Nodes["example.com/m"] = &model.Node{ID: "example.com/m", Kind: model.Local}
	g.Nodes["example.com/m/internal/db"] = &model.Node{ID: "example.com/m/internal/db", Kind: model.Local, Dir: "internal/db"}
	g.Edges = []model.Edge{{From: "example.com/m", To: "example.com/m/internal/db"}}
	return g
}

type fakeExtractor struct {
	g   *model.Graph
	err error
}

func (f *fakeExtractor) Extract(context.Context, string) (*model.Graph, error) { return f.g, f.err }

type fixedBase struct{ g *model.Graph }

func (f fixedBase) Graph(context.Context, string, string) (*model.Graph, string, error) {
	return f.g, "0123456789abcdef", nil
}

func TestNoBaseModeShowsWarning(t *testing.T) {
	p := &Pipeline{Extractor: &fakeExtractor{g: handGraph()}, Engine: newEngine(t), BaseWarning: "No base: not a git repository"}
	v, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v.Banner != p.BaseWarning || v.BaseSHA != "" {
		t.Errorf("banner = %q, sha = %q", v.Banner, v.BaseSHA)
	}
	if !v.Diff.Summary().Empty() {
		t.Errorf("summary = %+v, want no changes", v.Diff.Summary())
	}
}

func TestRefreshKeepsLastGoodViewOnError(t *testing.T) {
	ex := &fakeExtractor{g: handGraph()}
	s := New(&Pipeline{Extractor: ex, Engine: newEngine(t)})
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	good := s.Current()

	ex.err = errors.New("boom")
	if err := s.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh succeeded, want error")
	}
	v := s.Current()
	if v.Layout != good.Layout {
		t.Error("layout replaced after a failed refresh")
	}
	if !strings.HasPrefix(v.Banner, "Stale: ") || !strings.Contains(v.Banner, "boom") {
		t.Errorf("banner = %q", v.Banner)
	}

	ex.err = nil
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.Current().Banner != "" {
		t.Errorf("banner = %q after recovery, want none", s.Current().Banner)
	}
}

func TestRefreshBeforeAnyGoodView(t *testing.T) {
	s := New(&Pipeline{Extractor: &fakeExtractor{err: errors.New("no go.mod")}, Engine: newEngine(t)})
	if err := s.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh succeeded, want error")
	}
	if v := s.Current(); !v.Empty() || !strings.HasPrefix(v.Banner, "Error: ") {
		t.Errorf("view = %+v", v)
	}
}

func TestHandlers(t *testing.T) {
	s := New(&Pipeline{Extractor: &fakeExtractor{g: handGraph()}, Engine: newEngine(t)})
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	for _, tc := range []struct {
		path, want string
		code       int
	}{
		{"/", `sse-connect="/events"`, http.StatusOK},
		{"/node/example.com/m/internal/db", "internal/db", http.StatusOK},
		{"/node/example.com/m/nope", "", http.StatusNotFound},
		{"/static/app.js", "archdiag", http.StatusOK},
		{"/nope", "", http.StatusNotFound},
	} {
		resp, err := http.Get(ts.URL + tc.path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != tc.code || !strings.Contains(string(body), tc.want) {
			t.Errorf("GET %s = %d, body contains %q: %v", tc.path, resp.StatusCode, tc.want, strings.Contains(string(body), tc.want))
		}
	}
}

func TestHubKeepsOnlyLatestMessage(t *testing.T) {
	h := newHub()
	ch := h.subscribe()
	h.publish([]byte("a"))
	h.publish([]byte("b"))
	if got := string(<-ch); got != "b" {
		t.Errorf("got %q, want b", got)
	}
	h.unsubscribe(ch)
	h.publish([]byte("c")) // must not block or panic
}

func TestSSEEventFormat(t *testing.T) {
	got := string(sseEvent("diagram", []byte("<a>\n<b>")))
	if want := "event: diagram\ndata: <a>\ndata: <b>\n\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

type event struct{ name, data string }

func readEvents(body io.Reader) <-chan event {
	out := make(chan event)
	go func() {
		defer close(out)
		sc := bufio.NewScanner(body)
		sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
		var ev event
		var data []string
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				ev.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data = append(data, strings.TrimPrefix(line, "data: "))
			case line == "" && ev.name != "":
				ev.data = strings.Join(data, "\n")
				out <- ev
				ev, data = event{}, nil
			}
		}
	}()
	return out
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLiveUpdateOverSSE(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/live\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")

	base, err := extract.GoExtractor{}.Extract(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	s := New(&Pipeline{Dir: dir, BaseRef: "main", Base: fixedBase{base},
		Extractor: extract.GoExtractor{}, Engine: newEngine(t)})
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	w, err := watch.New(dir, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // runs first: ends the SSE request so ts.Close can return
	go s.Watch(ctx, w.Events(), func(string, ...any) {})

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type = %q", ct)
	}
	events := readEvents(resp.Body)

	first := <-events
	if first.name != "diagram" || strings.Contains(first.data, "st-added") {
		t.Fatalf("initial event = %s, want an unchanged diagram", first.name)
	}

	writeFile(t, filepath.Join(dir, "extra", "extra.go"), "package extra\n")
	writeFile(t, filepath.Join(dir, "main.go"), "package main\n\nimport _ \"example.com/live/extra\"\n\nfunc main() {}\n")

	want := `id="n-example.com/live/extra" class="node st-added`
	timeout := time.After(30 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("event stream closed")
			}
			if ev.name == "diagram" && strings.Contains(ev.data, want) {
				return
			}
		case <-timeout:
			t.Fatal("no diagram update with the new package")
		}
	}
}
