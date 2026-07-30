package client

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v0.1.0", "v0.2.0", true},
		{"v0.2.0", "v0.2.0", false},
		{"v0.3.0", "v0.2.0", false},
		{"v1.0.0", "v1.0.1", true},
		{"v1.2", "v1.2.0", false},
		{DevVersion, "v0.1.0", true},
		{"", "v0.1.0", true},
	}
	for _, c := range cases {
		if got := IsNewer(c.current, c.latest); got != c.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestParseChecksums(t *testing.T) {
	in := "abc123  page-report_v1.0.0_linux_amd64.tar.gz\n" +
		"def456 *page-report_v1.0.0_darwin_arm64.tar.gz\n" +
		"\n" +
		"garbage line with too many fields\n"
	sums, err := parseChecksums(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"page-report_v1.0.0_linux_amd64.tar.gz":  "abc123",
		"page-report_v1.0.0_darwin_arm64.tar.gz": "def456",
	}
	for k, v := range want {
		if sums[k] != v {
			t.Errorf("sums[%q] = %q, want %q", k, sums[k], v)
		}
	}
	if len(sums) != len(want) {
		t.Errorf("parsed %d entries, want %d: %v", len(sums), len(want), sums)
	}

	if _, err := parseChecksums(strings.NewReader("")); err == nil {
		t.Error("empty checksums must be an error")
	}
}

func TestAssetName(t *testing.T) {
	u := &Updater{OS: "darwin", Arch: "arm64"}
	if got, want := u.AssetName("v1.2.3"), "page-report_v1.2.3_darwin_arm64.tar.gz"; got != want {
		t.Errorf("AssetName = %q, want %q", got, want)
	}
}

// fakeRelease serves a GitHub-shaped release whose tarball holds a stand-in
// page-report binary printing reportedVersion from `version --json`.
type fakeRelease struct {
	tag             string
	reportedVersion string
	corruptChecksum bool
	omitChecksums   bool
	assetOS         string
	assetArch       string
}

func (f fakeRelease) serve(t *testing.T) *httptest.Server {
	t.Helper()
	if f.assetOS == "" {
		f.assetOS, f.assetArch = runtime.GOOS, runtime.GOARCH
	}
	asset := fmt.Sprintf("page-report_%s_%s_%s.tar.gz", f.tag, f.assetOS, f.assetArch)
	tarball := tarGz(t, "page-report", fakeBinary(f.reportedVersion))

	sum := sha256.Sum256(tarball)
	hexSum := hex.EncodeToString(sum[:])
	if f.corruptChecksum {
		hexSum = strings.Repeat("0", len(hexSum))
	}

	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/repos/o/r/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		assets := fmt.Sprintf(`{"name":%q,"browser_download_url":"%s/dl/%s"}`, asset, srv.URL, asset)
		if !f.omitChecksums {
			assets += fmt.Sprintf(`,{"name":"checksums.txt","browser_download_url":"%s/dl/checksums.txt"}`, srv.URL)
		}
		fmt.Fprintf(w, `{"tag_name":%q,"assets":[%s]}`, f.tag, assets)
	})
	mux.HandleFunc("/dl/"+asset, func(w http.ResponseWriter, _ *http.Request) {
		w.Write(tarball)
	})
	mux.HandleFunc("/dl/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", hexSum, asset)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func fakeBinary(version string) []byte {
	return []byte("#!/bin/sh\nprintf '{\"version\":\"" + version + "\"}\\n'\n")
}

func tarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	for _, w := range []interface{ Close() error }{tw, gz} {
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return buf.Bytes()
}

// newTestUpdater points an Updater at the fake release server.
func newTestUpdater(srv *httptest.Server) *Updater {
	u := NewUpdater()
	u.Repo = "o/r"
	u.APIBase = srv.URL
	u.HTTP = srv.Client()
	return u
}

// installTarget writes a stand-in "installed" binary and returns its path.
func installTarget(t *testing.T) string {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "page-report")
	if err := os.WriteFile(dest, fakeBinary("v0.1.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dest
}

func requireExecScripts(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("self-update is unsupported on windows")
	}
}

func TestLatestAndInstall(t *testing.T) {
	requireExecScripts(t)

	srv := fakeRelease{tag: "v0.2.0", reportedVersion: "v0.2.0"}.serve(t)
	u := newTestUpdater(srv)
	dest := installTarget(t)

	rel, err := u.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != "v0.2.0" {
		t.Fatalf("tag = %q, want v0.2.0", rel.Tag)
	}
	if !IsNewer("v0.1.0", rel.Tag) {
		t.Fatal("v0.2.0 must be newer than v0.1.0")
	}

	if err := u.Install(context.Background(), rel, dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, fakeBinary("v0.2.0")) {
		t.Fatalf("installed binary = %q, want the v0.2.0 payload", got)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o755 {
		t.Fatalf("installed binary mode = %o, want 755", perm)
	}
	if entries, _ := filepath.Glob(filepath.Join(filepath.Dir(dest), ".*")); len(entries) != 0 {
		t.Fatalf("staging files left behind: %v", entries)
	}
}

func TestInstallChecksumMismatchKeepsOldBinary(t *testing.T) {
	requireExecScripts(t)

	srv := fakeRelease{tag: "v0.2.0", reportedVersion: "v0.2.0", corruptChecksum: true}.serve(t)
	u := newTestUpdater(srv)
	dest := installTarget(t)

	rel, err := u.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	err = u.Install(context.Background(), rel, dest)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("Install error = %v, want a checksum mismatch", err)
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, fakeBinary("v0.1.0")) {
		t.Fatal("failed update must leave the installed binary untouched")
	}
	if entries, _ := filepath.Glob(filepath.Join(filepath.Dir(dest), ".*")); len(entries) != 0 {
		t.Fatalf("staging files left behind: %v", entries)
	}
}

func TestInstallMissingAssets(t *testing.T) {
	requireExecScripts(t)

	t.Run("no asset for platform", func(t *testing.T) {
		srv := fakeRelease{tag: "v0.2.0", assetOS: "plan9", assetArch: "mips"}.serve(t)
		u := newTestUpdater(srv)
		rel, err := u.Latest(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		err = u.Install(context.Background(), rel, installTarget(t))
		if err == nil || !strings.Contains(err.Error(), "no prebuilt binary") {
			t.Fatalf("Install error = %v, want 'no prebuilt binary'", err)
		}
	})

	t.Run("no checksums", func(t *testing.T) {
		srv := fakeRelease{tag: "v0.2.0", omitChecksums: true}.serve(t)
		u := newTestUpdater(srv)
		rel, err := u.Latest(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		err = u.Install(context.Background(), rel, installTarget(t))
		if err == nil || !strings.Contains(err.Error(), "checksums.txt") {
			t.Fatalf("Install error = %v, want a missing checksums.txt error", err)
		}
	})
}

func TestInstallReadOnlyDirFailsBeforeDownload(t *testing.T) {
	requireExecScripts(t)
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}

	var downloads int
	srv := fakeRelease{tag: "v0.2.0", reportedVersion: "v0.2.0"}.serve(t)
	u := newTestUpdater(srv)
	u.HTTP = &http.Client{Transport: countingTransport{srv.Client().Transport, &downloads}}

	dir := t.TempDir()
	dest := filepath.Join(dir, "page-report")
	if err := os.WriteFile(dest, fakeBinary("v0.1.0"), 0o755); err != nil {
		t.Fatal(err)
	}

	rel, err := u.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	before := downloads
	err = u.Install(context.Background(), rel, dest)
	if err == nil || !strings.Contains(err.Error(), "cannot write to") {
		t.Fatalf("Install error = %v, want 'cannot write to'", err)
	}
	// Only checksums.txt may have been fetched; the tarball must not be.
	if downloads-before > 1 {
		t.Fatalf("downloaded %d assets after the write check, want at most 1", downloads-before)
	}
}

type countingTransport struct {
	base http.RoundTripper
	n    *int
}

func (c countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if strings.HasPrefix(r.URL.Path, "/dl/") {
		*c.n++
	}
	return c.base.RoundTrip(r)
}

func TestRunnableRejectsGarbage(t *testing.T) {
	requireExecScripts(t)

	path := filepath.Join(t.TempDir(), "page-report")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho not json\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := runnable(context.Background(), path); err == nil {
		t.Fatal("runnable must reject a binary that does not report a version")
	}
}
