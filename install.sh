#!/bin/sh
# Install the page-report CLI from the latest GitHub release.
#
#   curl -fsSL https://raw.githubusercontent.com/dusansimic/page-report/main/install.sh | sh
#
# Environment:
#   PR_VERSION       release tag to install, e.g. v0.2.0 (default: latest)
#   PR_INSTALL_DIR   target directory (default: $HOME/.local/bin)
#
# Nothing is written outside the temp dir until the download is verified
# against the release checksums.
set -eu

REPO=dusansimic/page-report
BIN=page-report
INSTALL_DIR=${PR_INSTALL_DIR:-${HOME}/.local/bin}

info() { printf '%s\n' "$*" >&2; }
die() { printf 'install: %s\n' "$*" >&2; exit 1; }

need() {
	command -v "$1" >/dev/null 2>&1 || die "$1 is required but not installed"
}

# platform prints the <os>_<arch> pair used in release asset names.
platform() {
	os=$(uname -s)
	arch=$(uname -m)
	case "$os" in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) die "unsupported operating system '$os' (supported: Linux, Darwin)" ;;
	esac
	case "$arch" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) die "unsupported architecture '$arch' (supported: x86_64, arm64)" ;;
	esac
	printf '%s_%s\n' "$os" "$arch"
}

# latest_tag resolves the newest release tag. The /releases/latest URL
# redirects to /releases/tag/<tag>, so reading the effective URL avoids both
# the rate-limited API and a JSON parser.
latest_tag() {
	url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
		"https://github.com/${REPO}/releases/latest")
	tag=${url##*/}
	case "$tag" in
	'' | latest) die "could not determine the latest release of $REPO" ;;
	esac
	printf '%s\n' "$tag"
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d' ' -f1
	else
		die "need sha256sum or shasum to verify the download"
	fi
}

need curl
need tar
need uname

plat=$(platform)
tag=${PR_VERSION:-$(latest_tag)}
asset="${BIN}_${tag}_${plat}.tar.gz"
base="https://github.com/${REPO}/releases/download/${tag}"

tmp=$(mktemp -d)
staged=
cleanup() {
	rm -rf "$tmp"
	[ -n "$staged" ] && rm -f "$staged"
	return 0
}
trap cleanup EXIT INT TERM

info "Downloading $asset"
curl -fsSL -o "$tmp/$asset" "$base/$asset" ||
	die "no release asset $asset (check the tag and your platform)"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" ||
	die "could not download checksums.txt for $tag"

want=$(awk -v f="$asset" '$2 == f || $2 == "*" f { print $1 }' "$tmp/checksums.txt")
[ -n "$want" ] || die "$asset is not listed in checksums.txt"
got=$(sha256 "$tmp/$asset")
[ "$want" = "$got" ] || die "checksum mismatch for $asset (want $want, got $got)"

tar -xzf "$tmp/$asset" -C "$tmp" "$BIN" || die "could not extract $BIN from $asset"

mkdir -p "$INSTALL_DIR" || die "could not create $INSTALL_DIR"
# Stage next to the target and rename: rename(2) is atomic and, unlike
# truncating in place, safe while the old binary is running.
staged="$INSTALL_DIR/.$BIN.new.$$"
cp "$tmp/$BIN" "$staged" || die "$INSTALL_DIR is not writable"
chmod 0755 "$staged"
mv -f "$staged" "$INSTALL_DIR/$BIN"
staged=

case ":$PATH:" in
*":$INSTALL_DIR:"*) ;;
*)
	info ""
	info "note: $INSTALL_DIR is not in your PATH. Add it with:"
	info "  export PATH=\"$INSTALL_DIR:\$PATH\""
	;;
esac

info "Installed to $INSTALL_DIR/$BIN"
"$INSTALL_DIR/$BIN" version | head -1
