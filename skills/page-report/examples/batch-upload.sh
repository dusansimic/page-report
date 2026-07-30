#!/bin/sh
# Upload every HTML file in a directory, continuing past failures.
#
#   ./batch-upload.sh ./reports '*.html'
#
# Prints "<file> -> <url>" per success and a summary of failures at the end.
# Uploads are not idempotent: running this twice creates duplicate pages.
set -eu

DIR="${1:?usage: $0 <directory> [glob]}"
PATTERN="${2:-*.html}"

if ! page-report list --json >/dev/null 2>&1; then
	echo "Not authenticated (or server URL unset). Run: page-report login" >&2
	exit 1
fi

failed=0
for file in "$DIR"/$PATTERN; do
	[ -f "$file" ] || continue
	title=$(basename "$file" .html)
	if url=$(page-report upload "$file" --title "$title"); then
		echo "$file -> $url"
	else
		echo "failed: $file" >&2
		failed=$((failed + 1))
	fi
done

[ "$failed" -eq 0 ] || {
	echo "$failed upload(s) failed." >&2
	exit 1
}
