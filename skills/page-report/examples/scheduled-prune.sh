#!/bin/sh
# Delete pages older than a retention window. Meant for cron/CI, not ad-hoc use.
#
#   ./scheduled-prune.sh 30d          # delete, after listing what will go
#   DRY_RUN=1 ./scheduled-prune.sh 30d  # list only
#
# WARNING: prune is server-wide, not scoped to the calling user, and has no
# confirmation prompt. It deletes everyone's pages older than the threshold.
# Confirm the retention policy with a human before scheduling this.
set -eu

OLDER_THAN="${1:-30d}"
DRY_RUN="${DRY_RUN:-0}"

if ! page-report list --json >/dev/null 2>&1; then
	echo "Not authenticated (or server URL unset). Run: page-report login" >&2
	exit 1
fi

echo "Pages currently on the server:"
page-report list

if [ "$DRY_RUN" != "0" ]; then
	echo "DRY_RUN set; not pruning (--older-than $OLDER_THAN)."
	exit 0
fi

page-report prune --older-than "$OLDER_THAN"
