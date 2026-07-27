// Package web holds embedded web assets: static files served verbatim by the
// app domain, and HTML templates rendered by internal/server.
package web

import "embed"

//go:embed all:static
var StaticFS embed.FS
