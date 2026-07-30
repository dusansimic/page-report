#!/bin/sh
# End-to-end agent loop: render a report, publish it, keep the id for later.
#
#   ./agent-workflow.sh "Nightly build report"
#
# Shows the shape an agent should follow: build a self-contained HTML file in a
# scratch dir, upload with --json so the id is available for a later delete,
# print the URL as the result.
set -eu

TITLE="${1:-Report $(date +%Y-%m-%d)}"
WORKDIR=$(mktemp -d)
REPORT="$WORKDIR/report.html"
trap 'rm -rf "$WORKDIR"' EXIT

# Self-contained: inline CSS only, no external assets (server CSP blocks them).
cat >"$REPORT" <<EOF
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>$TITLE</title>
<style>
  body { font: 16px/1.6 system-ui, sans-serif; max-width: 46rem; margin: 2rem auto; padding: 0 1rem; }
  @media (prefers-color-scheme: dark) { body { background: #16181d; color: #e6e8eb; } }
</style>
</head>
<body>
<h1>$TITLE</h1>
<p>Generated at $(date -u +%Y-%m-%dT%H:%M:%SZ).</p>
</body>
</html>
EOF

if ! page-report list --json >/dev/null 2>&1; then
	echo "Not authenticated (or server URL unset). Run: page-report login" >&2
	exit 1
fi

# --json keeps the id; the page is immutable, so replacing it later means a new
# upload plus an explicit `page-report delete <id>` of this one.
out=$(page-report upload "$REPORT" --title "$TITLE" --json)
id=$(printf '%s' "$out" | sed -n 's/.*"id": *"\([^"]*\)".*/\1/p')
url=$(printf '%s' "$out" | sed -n 's/.*"url": *"\([^"]*\)".*/\1/p')

echo "id:  $id"
echo "url: $url"
