// Package server runs the extract → diff → layout pipeline and serves the
// live UI over HTTP with server-sent events.
package server

import (
	"context"
	"fmt"

	"codebase/arch/internal/diff"
	"codebase/arch/internal/extract"
	"codebase/arch/internal/layout"
	"codebase/arch/internal/model"
	"codebase/arch/internal/web"
)

// BaseSource provides the base graph for a ref. *gitbase.Builder implements it.
type BaseSource interface {
	Graph(ctx context.Context, dir, ref string) (*model.Graph, string, error)
}

// Layouter positions a diff graph. *layout.Engine implements it.
type Layouter interface {
	Layout(ctx context.Context, d *diff.DiffGraph) (*layout.Layout, error)
}

// Pipeline turns the working tree at Dir into a View.
type Pipeline struct {
	Dir         string
	BaseRef     string
	Base        BaseSource // nil: no-base mode, current is compared with itself
	BaseWarning string     // banner shown in no-base mode
	Extractor   extract.Extractor
	Engine      Layouter
	Filter      model.FilterOptions
}

// Run extracts the working tree, compares it with the base and lays it out.
func (p *Pipeline) Run(ctx context.Context) (web.View, error) {
	raw, err := p.Extractor.Extract(ctx, p.Dir)
	if err != nil {
		return web.View{}, fmt.Errorf("extract working tree: %w", err)
	}
	cur := model.Filter(raw, p.Filter)

	base, sha := cur, ""
	if p.Base != nil {
		bg, s, err := p.Base.Graph(ctx, p.Dir, p.BaseRef)
		if err != nil {
			return web.View{}, fmt.Errorf("build base %q: %w", p.BaseRef, err)
		}
		base, sha = model.Filter(bg, p.Filter), s
	}

	d := diff.Compute(base, cur)
	l, err := p.Engine.Layout(ctx, d)
	if err != nil {
		return web.View{}, fmt.Errorf("lay out diagram: %w", err)
	}
	return web.View{Module: cur.Module, BaseRef: p.BaseRef, BaseSHA: sha,
		Banner: p.BaseWarning, Diff: d, Layout: l}, nil
}
