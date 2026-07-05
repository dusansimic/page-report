// Package web holds embedded static assets served by the app domain.
package web

import "embed"

//go:embed all:static
var StaticFS embed.FS
