package web

import "embed"

// Static holds the front-end assets under "static/"; serve it at /static/.
//
//go:embed static
var Static embed.FS

// diagramCSS is inlined into standalone SVGs so they render without the app.
//
//go:embed static/diagram.css
var diagramCSS string
