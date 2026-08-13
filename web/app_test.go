package web

import (
	"io/fs"
	"testing"
)

// The embed directive needs at least one file under app/dist to resolve, and
// dist is a build artifact. A committed placeholder is what lets `go build`
// succeed without a Node toolchain; losing it turns a clean checkout into a
// compile error, so guard it here rather than discovering it in CI.
//
// It exists twice on purpose: tracked at app/dist/.gitkeep, and at
// app/public/.gitkeep so that Vite re-copies it after emptying dist.
func TestPlaceholderKeepsEmbedResolvable(t *testing.T) {
	if _, err := fs.Stat(AppFS, "app/dist/.gitkeep"); err != nil {
		t.Fatalf("app/dist/.gitkeep is missing from the embedded FS: %v\n"+
			"Restore it (and web/app/public/.gitkeep): without it `go build` "+
			"fails on a checkout that has no frontend build.", err)
	}
}
