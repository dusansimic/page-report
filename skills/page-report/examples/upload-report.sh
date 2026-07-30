#!/bin/sh
# Upload one HTML report and print its URL.
#
#   ./upload-report.sh report.html "Weekly metrics"
#
# The title defaults to the filename without extension, which is rarely a good
# page title -- pass one.
set -eu

REPORT_FILE="${1:?usage: $0 <report.html> [title]}"
TITLE="${2:-$(basename "$REPORT_FILE" .html)}"

# Cheap auth probe: `list` is read-only and fails fast when unauthenticated.
# Login blocks on browser approval, so ask the user instead of running it here.
if ! page-report list --json >/dev/null 2>&1; then
	echo "Not authenticated (or server URL unset). Run: page-report login" >&2
	exit 1
fi

page-report upload "$REPORT_FILE" --title "$TITLE"
