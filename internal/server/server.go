package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"codebase/arch/internal/web"
)

// Server holds the latest view and pushes updates to browsers.
type Server struct {
	pipeline *Pipeline
	hub      *hub

	mu   sync.RWMutex
	view web.View
	good bool // view came from a successful run
}

// New returns a server for p. Call Refresh before serving.
func New(p *Pipeline) *Server {
	return &Server{pipeline: p, hub: newHub(), view: web.View{Banner: "Loading…"}}
}

// Current returns the view being shown.
func (s *Server) Current() web.View {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.view
}

// Refresh re-runs the pipeline and pushes the result to connected browsers.
// On failure the last good diagram stays up with a "Stale" banner, and the
// error is returned.
func (s *Server) Refresh(ctx context.Context) error {
	v, err := s.pipeline.Run(ctx)
	s.mu.Lock()
	switch {
	case err == nil:
		s.view, s.good = v, true
	case s.good:
		s.view.Banner = "Stale: " + err.Error()
	default:
		s.view = web.View{Banner: "Error: " + err.Error()}
	}
	view := s.view
	s.mu.Unlock()

	msg, rerr := renderUpdate(ctx, view)
	if rerr != nil {
		return errors.Join(err, fmt.Errorf("render update: %w", rerr))
	}
	s.hub.publish(msg)
	return err
}

// Watch refreshes once per signal until ctx ends or events closes. Runs never
// overlap; a signal that arrives mid-run (the watcher buffers one) causes
// exactly one more run.
func (s *Server) Watch(ctx context.Context, events <-chan struct{}, logf func(string, ...any)) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-events:
			if !ok {
				return
			}
			if err := s.Refresh(ctx); err != nil {
				logf("refresh: %v", err)
			}
		}
	}
}

// Handler serves the page, the event stream, node details and static assets.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handlePage)
	mux.HandleFunc("GET /events", s.handleEvents)
	mux.HandleFunc("GET /node/{id...}", s.handleNode)
	mux.Handle("GET /static/", http.FileServerFS(web.Static))
	return mux
}

func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = web.Page(s.Current()).Render(r.Context(), w)
}

func (s *Server) handleNode(w http.ResponseWriter, r *http.Request) {
	d, ok := s.Current().Details(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = web.Details(d).Render(r.Context(), w)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	// Subscribe before rendering the initial state so no update is missed.
	ch := s.hub.subscribe()
	defer s.hub.unsubscribe(ch)
	initial, err := renderUpdate(r.Context(), s.Current())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	if _, err := w.Write(initial); err != nil {
		return
	}
	flusher.Flush()

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		var msg []byte
		select {
		case <-r.Context().Done():
			return
		case msg = <-ch:
		case <-ping.C:
			msg = []byte(": ping\n\n")
		}
		if _, err := w.Write(msg); err != nil {
			return
		}
		flusher.Flush()
	}
}

// renderUpdate renders the two live regions as SSE events.
func renderUpdate(ctx context.Context, v web.View) ([]byte, error) {
	var canvas, summary bytes.Buffer
	if err := web.Canvas(v).Render(ctx, &canvas); err != nil {
		return nil, err
	}
	if err := web.Summary(v).Render(ctx, &summary); err != nil {
		return nil, err
	}
	return append(sseEvent("diagram", canvas.Bytes()), sseEvent("summary", summary.Bytes())...), nil
}
