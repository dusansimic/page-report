package web

import "embed"

// TemplatesFS holds html/template sources rendered by internal/server. Kept
// separate from StaticFS because StaticFS is served verbatim by the app
// domain's file server — template sources must not be publicly fetchable.
//
//go:embed templates
var TemplatesFS embed.FS
