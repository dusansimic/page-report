---
name: page-report
description: Publish static HTML reports to a page-report server and share the URL. Use when the user asks to publish, share, or upload a generated HTML report page.
---

# Publishing HTML reports with page-report

## Prerequisites

- The `pr` binary is in PATH.
- The server URL is configured: either `PR_SERVER_URL` is set in the
  environment, or pass `--server <app-domain-url>` to every command.

## One-time login

Authentication uses an OAuth device flow. Run:

```sh
pr login
```

The command prints a verification URL and a code, for example:

```
Open https://github.com/login/device and enter code: ABCD-1234
```

**Relay the URL and code to the user verbatim and wait** — the user must
approve the login in their browser. The command blocks until approval, then
stores credentials locally. Login persists across sessions; only repeat it
when a command fails with an authentication error.

## Publishing a report

```sh
pr upload report.html --title "Weekly metrics"
```

On success, stdout is a single line: the shareable URL of the page.
**Always show this URL to the user** — it is the deliverable. The viewer must
authenticate against the same identity provider to open it.

Use `--json` to get `{"id": "...", "url": "..."}` instead.

## Managing pages

```sh
pr list                    # table of all pages (--json for JSON)
pr delete <id>             # remove one page
pr prune --older-than 30d  # remove pages older than a duration (e.g. 30d, 720h)
```

## Troubleshooting

- `unauthenticated` error / hint about login → run `pr login` again and relay
  the verification code to the user.
- "server URL required" → set `PR_SERVER_URL` or pass `--server`.
