// Command archdiag draws a live architecture diagram of a Go module and
// highlights how it differs from a git base ref.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"codebase/arch/internal/extract"
	"codebase/arch/internal/gitbase"
	"codebase/arch/internal/layout"
	"codebase/arch/internal/model"
	"codebase/arch/internal/server"
	"codebase/arch/internal/watch"
	"codebase/arch/internal/web"
)

const usageText = `Usage:
  archdiag serve  [flags] [dir]   serve a live diagram in the browser
  archdiag render [flags] [dir]   write the diff diagram as SVG to stdout

Flags (before dir):
  --base ref    git ref to compare with (default "main")
  --std         show standard-library packages
  --deps mode   third-party packages: collapsed, hidden or full (default "collapsed")
  --addr addr   listen address, serve only (default "127.0.0.1:7777")
`

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch args[0] {
	case "serve":
		err = serve(ctx, args[1:], stderr)
	case "render":
		err = render(ctx, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usageText)
		return 0
	default:
		fmt.Fprintf(stderr, "archdiag: unknown command %q\n\n%s", args[0], usageText)
		return 2
	}
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return 0
	default:
		fmt.Fprintln(stderr, "archdiag:", err)
		return 1
	}
}

type options struct {
	dir, base, deps, addr string
	showStd               bool
}

func parseFlags(name string, args []string, stderr io.Writer, withAddr bool) (options, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o options
	fs.StringVar(&o.base, "base", "main", "git ref to compare with")
	fs.BoolVar(&o.showStd, "std", false, "show standard-library packages")
	fs.StringVar(&o.deps, "deps", string(model.DepsCollapsed), "third-party packages: collapsed, hidden or full")
	if withAddr {
		fs.StringVar(&o.addr, "addr", "127.0.0.1:7777", "listen address")
	}
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	switch fs.NArg() {
	case 0:
		o.dir = "."
	case 1:
		o.dir = fs.Arg(0)
	default:
		return o, fmt.Errorf("expected at most one directory, got %d", fs.NArg())
	}
	return o, nil
}

// newPipeline wires the pipeline. With requireBase, an unresolvable base ref
// is an error; otherwise it falls back to no-base mode.
func newPipeline(ctx context.Context, o options, engine *layout.Engine, requireBase bool) (*server.Pipeline, error) {
	deps, err := model.ParseDepsMode(o.deps)
	if err != nil {
		return nil, err
	}
	p := &server.Pipeline{
		Dir: o.dir, BaseRef: o.base, Extractor: extract.GoExtractor{}, Engine: engine,
		Filter: model.FilterOptions{ShowStd: o.showStd, Deps: deps},
	}
	if _, err := gitbase.ResolveRef(ctx, o.dir, o.base); err != nil {
		if requireBase {
			return nil, fmt.Errorf("resolve base: %w", err)
		}
		p.BaseWarning = fmt.Sprintf("No base (%v): showing the current graph only.", err)
		return p, nil
	}
	cacheDir, err := gitbase.DefaultCacheDir()
	if err != nil {
		cacheDir = filepath.Join(os.TempDir(), "archdiag")
	}
	_ = gitbase.Prune(ctx, o.dir) // best effort: clears worktrees left by crashed runs
	p.Base = &gitbase.Builder{Extractor: extract.GoExtractor{}, CacheDir: cacheDir}
	return p, nil
}

func render(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	o, err := parseFlags("render", args, stderr, false)
	if err != nil {
		return err
	}
	engine, err := layout.New(ctx)
	if err != nil {
		return err
	}
	defer engine.Close()
	p, err := newPipeline(ctx, o, engine, true)
	if err != nil {
		return err
	}
	v, err := p.Run(ctx)
	if err != nil {
		return err
	}
	return web.Diagram(v.Layout, true).Render(ctx, stdout)
}

func serve(ctx context.Context, args []string, stderr io.Writer) error {
	o, err := parseFlags("serve", args, stderr, true)
	if err != nil {
		return err
	}
	logger := log.New(stderr, "archdiag: ", 0)
	engine, err := layout.New(ctx)
	if err != nil {
		return err
	}
	defer engine.Close()
	p, err := newPipeline(ctx, o, engine, false)
	if err != nil {
		return err
	}
	if p.Base == nil {
		logger.Print(p.BaseWarning)
	}

	srv := server.New(p)
	if err := srv.Refresh(ctx); err != nil {
		logger.Printf("initial build: %v", err)
	}
	w, err := watch.New(o.dir, 300*time.Millisecond)
	if err != nil {
		return err
	}
	defer w.Close()
	go srv.Watch(ctx, w.Events(), logger.Printf)

	ln, err := net.Listen("tcp", o.addr)
	if err != nil {
		return err
	}
	hs := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = hs.Shutdown(shutdownCtx)
	}()
	logger.Printf("serving %s on http://%s", o.dir, ln.Addr())
	if err := hs.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
