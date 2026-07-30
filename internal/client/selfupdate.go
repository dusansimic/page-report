package client

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const (
	// defaultRepo is the GitHub repository releases are pulled from.
	defaultRepo = "dusansimic/page-report"
	// defaultAPIBase is overridden in tests to point at an httptest server.
	defaultAPIBase = "https://api.github.com"
	// binaryName is both the executable name and the asset name prefix.
	binaryName = "page-report"
	// checksumsAsset lists the sha256 of every tarball in a release.
	checksumsAsset = "checksums.txt"
	// maxAssetBytes caps how much of a downloaded tarball is read, so a
	// malformed or hostile asset cannot fill the disk before the checksum
	// is even known.
	maxAssetBytes = 256 << 20
)

// DevVersion is the version string of binaries built straight from source
// (no ldflags, no module version). Such builds are not updatable.
const DevVersion = "dev"

// Release is the subset of a GitHub release the updater needs.
type Release struct {
	Tag    string
	Assets map[string]string // asset name -> download URL
}

// Updater downloads released page-report binaries from GitHub and installs
// them over the running executable. The zero value is not usable; call
// NewUpdater.
type Updater struct {
	Repo    string // "owner/name"
	APIBase string // GitHub API base URL
	HTTP    *http.Client
	OS      string // target GOOS
	Arch    string // target GOARCH
}

func NewUpdater() *Updater {
	return &Updater{
		Repo:    defaultRepo,
		APIBase: defaultAPIBase,
		HTTP:    &http.Client{Timeout: 5 * time.Minute},
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
	}
}

// AssetName is the release asset holding the binary for the target platform,
// matching the naming the release workflow and install.sh use.
func (u *Updater) AssetName(tag string) string {
	return fmt.Sprintf("%s_%s_%s_%s.tar.gz", binaryName, tag, u.OS, u.Arch)
}

// Latest fetches the most recent published release.
func (u *Updater) Latest(ctx context.Context) (Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", strings.TrimRight(u.APIBase, "/"), u.Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := u.HTTP.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("query latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("query latest release: %s returned %s", url, resp.Status)
	}

	var payload struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&payload); err != nil {
		return Release{}, fmt.Errorf("decode release metadata: %w", err)
	}
	if payload.TagName == "" {
		return Release{}, errors.New("release metadata has no tag_name")
	}

	rel := Release{Tag: payload.TagName, Assets: make(map[string]string, len(payload.Assets))}
	for _, a := range payload.Assets {
		rel.Assets[a.Name] = a.URL
	}
	return rel, nil
}

// IsNewer reports whether latest is a strictly newer version than current.
// Non-semver current versions (notably "dev" and `git describe` output) are
// never considered up to date.
func IsNewer(current, latest string) bool {
	if !semver.IsValid(current) {
		return true
	}
	return semver.Compare(semver.Canonical(latest), semver.Canonical(current)) > 0
}

// Install downloads the release asset for the updater's platform, verifies it
// against the release checksums, and atomically replaces the binary at dest.
// The existing binary is only touched once the replacement is verified and
// has proven runnable, so a failed update always leaves dest intact.
func (u *Updater) Install(ctx context.Context, rel Release, dest string) error {
	if u.OS == "windows" {
		return errors.New("self-update is not supported on windows: reinstall from the release page")
	}
	name := u.AssetName(rel.Tag)
	assetURL, ok := rel.Assets[name]
	if !ok {
		return fmt.Errorf("release %s has no prebuilt binary for %s/%s (expected asset %s)",
			rel.Tag, u.OS, u.Arch, name)
	}
	sumsURL, ok := rel.Assets[checksumsAsset]
	if !ok {
		return fmt.Errorf("release %s has no %s to verify the download against", rel.Tag, checksumsAsset)
	}

	sums, err := u.checksums(ctx, sumsURL)
	if err != nil {
		return err
	}
	want, ok := sums[name]
	if !ok {
		return fmt.Errorf("%s is not listed in %s", name, checksumsAsset)
	}

	// Staging next to dest keeps the rename on one filesystem, which is what
	// makes the swap atomic; creating it first also fails early (before the
	// download) when the install dir is not writable.
	staged := filepath.Join(filepath.Dir(dest), "."+filepath.Base(dest)+".new")
	f, err := os.OpenFile(staged, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w\nhint: reinstall with install.sh or your package manager",
			filepath.Dir(dest), err)
	}
	defer os.Remove(staged) // no-op once the rename succeeded

	if err := u.extract(ctx, assetURL, want, f); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write %s: %w", staged, err)
	}
	// O_CREATE applies the umask, so set the mode explicitly.
	if err := os.Chmod(staged, 0o755); err != nil {
		return err
	}
	if err := runnable(ctx, staged); err != nil {
		return err
	}
	if err := os.Rename(staged, dest); err != nil {
		return fmt.Errorf("replace %s: %w", dest, err)
	}
	return nil
}

// extract streams the tarball into w, taking the binary member only, and
// fails if the sha256 of the whole asset does not match want. The stream is
// hashed as it is read, so verification covers exactly the bytes used.
func (u *Updater) extract(ctx context.Context, assetURL, want string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return err
	}
	resp, err := u.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", assetURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", assetURL, resp.Status)
	}

	h := sha256.New()
	tee := io.TeeReader(io.LimitReader(resp.Body, maxAssetBytes), h)
	gz, err := gzip.NewReader(tee)
	if err != nil {
		return fmt.Errorf("read release tarball: %w", err)
	}
	defer gz.Close()

	found := false
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read release tarball: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || path.Base(hdr.Name) != binaryName {
			continue
		}
		if _, err := io.Copy(w, tr); err != nil {
			return fmt.Errorf("extract %s: %w", binaryName, err)
		}
		found = true
	}
	// Drain whatever the gzip reader left so the hash covers the full asset.
	if _, err := io.Copy(io.Discard, tee); err != nil {
		return fmt.Errorf("download %s: %w", assetURL, err)
	}

	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("checksum mismatch: want %s, got %s", want, got)
	}
	if !found {
		return fmt.Errorf("release tarball contains no %s binary", binaryName)
	}
	return nil
}

func (u *Updater) checksums(ctx context.Context, url string) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := u.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", checksumsAsset, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", checksumsAsset, resp.Status)
	}
	return parseChecksums(io.LimitReader(resp.Body, 1<<20))
}

// parseChecksums reads `sha256sum` output: "<hex>  <name>", where a leading
// "*" on the name marks binary mode.
func parseChecksums(r io.Reader) (map[string]string, error) {
	sums := map[string]string{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			continue
		}
		sums[strings.TrimPrefix(fields[1], "*")] = fields[0]
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", checksumsAsset, err)
	}
	if len(sums) == 0 {
		return nil, fmt.Errorf("%s is empty", checksumsAsset)
	}
	return sums, nil
}

// runnable sanity-checks the downloaded binary by asking it for its version.
// This catches a truncated or wrong-architecture asset that still matched its
// (equally wrong) checksum entry.
func runnable(ctx context.Context, path string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, path, "version", "--json").Output()
	if err != nil {
		return fmt.Errorf("downloaded binary is not runnable: %w", err)
	}
	var meta struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(out, &meta); err != nil || meta.Version == "" {
		return errors.New("downloaded binary did not report a version")
	}
	return nil
}

// ExecutablePath returns the path of the running binary with symlinks
// resolved, which is what an update has to replace.
func ExecutablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate the running binary: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", exe, err)
	}
	return resolved, nil
}
