// Package web holds the embedded frontend: the built React SPA that the
// server serves from the single origin.
//
// The dist directory is produced by `pnpm --dir web/app build` and is not
// committed. Only dist/.gitkeep is, so that `go build ./...` works on a clean
// checkout with no Node toolchain installed — the `all:` prefix below is what
// makes embed see that dotfile, since it skips dotfiles otherwise. A binary
// built without the frontend has an empty SPA, which the server reports at
// runtime rather than panicking at init.
//
// The placeholder also lives in web/app/public/.gitkeep. Vite copies publicDir
// into dist on every build, which restores it after `emptyOutDir` wipes the
// directory; without that, one frontend build would delete the file the
// no-Node build depends on. Keep both copies.
package web

import "embed"

//go:embed all:app/dist
var AppFS embed.FS
