---
name: page-report
description: Publish static HTML reports to a self-hosted page-report server and share the resulting URL. Use when the user asks to publish, share, or upload a generated HTML report, plan or status page, or to list, delete or prune pages already on the server.
keywords: [reports, html, publishing, oauth, page-sharing, page-report]
platforms: [linux, macos]
license: BSD-2-Clause
---

# Publishing HTML reports with page-report

`page-report` uploads a self-contained HTML file to a page-report server and
prints one URL. That URL is the deliverable: the user opens it, authenticates
against the server's identity provider, and reads the page.

Primary job: **write the report as HTML → `page-report upload` → show the URL.**

## Prerequisites

- **Binary in `PATH`.** Install with
  `curl -fsSL https://raw.githubusercontent.com/dusansimic/page-report/main/install.sh | sh`
  (installs into `~/.local/bin`; `PR_INSTALL_DIR` and `PR_VERSION` override
  destination and tag). Or build from source:
  `go build -o page-report ./cmd/page-report`. Linux and macOS, amd64 and
  arm64.
- **Server URL configured**, in any of three ways (highest precedence first):
  `--server <app-domain-url>` on every command, `PR_SERVER_URL` in the
  environment, or `server_url: <app-domain-url>` in
  `$XDG_CONFIG_HOME/page-report/config.yml` (default
  `~/.config/page-report/config.yml`). The value is the **app** domain; the
  the dashboard, the API and the page links all live on that one origin.
- **Logged in** — see below. The token is stored in
  `~/.config/page-report/credentials.json` (mode 0600) and persists across
  sessions.

## One-time login

The CLI authenticates with an API token the user creates in the server's web
dashboard. There is no device flow and nothing to poll.

```sh
page-report login
```

It prints where to get a token, then waits for it to be pasted (input hidden):

```
Create a token at https://reports.example.org/tokens
Paste it here (input is hidden):
```

**Give the user that URL and wait for them.** They sign in, click *New token*,
and copy it — a token is shown only once. Prefer having the user run
`page-report login` themselves; it cannot complete without them.

If the user hands you a token directly, store it without a prompt:

```sh
echo "$TOKEN" | page-report login --token-stdin
```

Or skip storing entirely — `PR_TOKEN` overrides the credentials file and is the
right choice for one-off and CI use:

```sh
PR_TOKEN=prt_... page-report upload report.html
```

`page-report whoami` shows which identity and token are in use. Repeat login
only when a command fails with an authentication error (a revoked, deleted or
expired token looks the same as no token). `page-report logout` deletes the
stored token.

## Publishing a report

```sh
page-report upload report.html --title "Weekly metrics"
# → https://pages.example.org/p/5gQG6fZu3S73
```

On success stdout is a single line: the shareable URL. **Always show this URL
to the user** — it is the deliverable. Take it from the command output rather
than hand-building it.

- `--title` defaults to the filename without extension, which is usually ugly.
  Always pass a real human title; it is what `list` and the page header show.
- `--json` prints `{"id": "...", "url": "..."}` — use it when you need the id
  for a later `delete`.
- Non-zero exit with `error: ...` on stderr means nothing was uploaded.

**Uploads are immutable.** There is no update command; a revision is a new
upload with a new URL. Pages are readable by everyone on the server allowlist,
so do not upload secrets, tokens, or third-party-confidential detail.

### Writing the HTML

Pages are served sandboxed, in an opaque origin, under a strict CSP:

```
sandbox allow-popups; default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:; form-action 'none'; base-uri 'none'; frame-ancestors 'none'
```

The file must therefore be **fully self-contained and free of JavaScript**:

- **`<script>` does not run.** Neither do inline `on*` handlers. The sandbox
  omits `allow-scripts`, so this is enforced by the server, not a convention.
  Write static HTML; render charts as inline SVG.
- Inline `<style>` is allowed (`unsafe-inline`). External CSS/JS/fonts/images
  from any CDN are **blocked** — no Tailwind CDN, no Google Fonts, no chart.js
  from unpkg. Inline it or skip it.
- Images: `data:` URIs only; inline SVG is fine.
- No forms, no `fetch`/XHR, and the page cannot be framed.
- Upload content type must be `text/html` or `text/plain`; anything else is
  rejected. The CLI always sends the right one.
- Size cap: 5 MiB by default (`max_upload_bytes` server config). Empty files
  are rejected.
- Write a complete document — `<!doctype html>`, `<head>`, `<title>`,
  `<meta name="viewport">`. Nothing wraps it for you. Style it for light and
  dark, mobile width included.

`templates/report-template.html` in this skill is a CSP-safe starting point.

## Command reference

| Command | What it does |
|---|---|
| `page-report login [--token-stdin]` | Stores an API token from the web dashboard. Prompts unless `--token-stdin`. |
| `page-report logout` | Deletes the stored token. |
| `page-report whoami [--json]` | Which identity and token the stored credentials belong to. |
| `page-report upload <file.html> [--title T] [--json]` | Publishes a page; prints its URL. |
| `page-report list [--json]` | Your own pages, newest first. |
| `page-report get <id> [-o F] [--meta] [--json]` | Downloads a page's HTML; stdout unless `-o`. |
| `page-report delete <id>` | Removes one page. No confirmation prompt. |
| `page-report prune --older-than <dur>` | Removes all pages older than `dur`. No confirmation prompt. |
| `page-report update [--check] [--force] [--json]` | Self-updates the binary from the latest GitHub release. |
| `page-report version [--json]` | Version, commit, build date, Go version, platform. |

Global flags: `--server <url>`, `--config <path>`. Anything else:
`page-report --help`, `page-report <cmd> --help`.

`list` without `--json` is a table (`ID CREATED SIZE CREATED_BY TITLE`); with
`--json` an array of `{id, title, size_bytes, created_at, created_by, url}`,
`created_at` being RFC 3339 UTC. See `templates/metadata.json` for the exact
shapes of every `--json` output.

`--older-than` accepts Go durations plus a `d` suffix: `30d`, `720h`, `90m`.

`get` writes the stored HTML to stdout by default, so it pipes; `-o <file>`
writes a file instead (refused if the file exists, unless `--force`) and the
confirmation goes to stderr. `--meta` fetches metadata only — same fields as
`list --json`, plus `content_type` — and `--json` is only valid together with
`--meta`. Use it to recover a report published in an earlier session: take the
id from `list`, or from the `/p/<id>` segment of the page URL.

`update` refuses to touch a `dev` build unless `--force`, verifies the
download against the release `checksums.txt` and test-runs it before replacing
the binary, and needs the install directory writable and on one filesystem.

### Destructive commands

`delete` and `prune` take effect immediately, have **no confirmation prompt**,
and are scoped to the token's owner: you cannot see or remove another user's
pages, but `prune` can still wipe every report *this* user published. **Ask the
user first**, naming the ids and titles you are about to remove. When replacing
an earlier revision, upload the new page first, then ask whether to delete the
old one.

## Patterns

- **Initial setup** — `page-report version` to confirm the binary, set
  `server_url` in the config file once, then `page-report login` (which needs a
  token from the dashboard) and `page-report whoami` to confirm.
- **Publish and share** — write HTML to a scratch file (not into the user's
  repo), `upload --title`, show the URL as the last line of your reply.
- **Batch** — loop `upload` over a directory; see `examples/batch-upload.sh`.
- **Cleanup** — `prune --older-than 30d` on a schedule, after confirming the
  policy with the user; see `examples/scheduled-prune.sh`.
- **Idempotency** — there is none: re-uploading the same file creates a second
  page with a new id. Track ids from `upload --json` if you need to replace.
- **Scripting** — parse `--json`, never the table. `jq -r '.[0].id'` etc.

Runnable versions of these live in `examples/`.

## Configuration

| Setting | Flag | Env var | Config key |
|---|---|---|---|
| Server base URL | `--server` | `PR_SERVER_URL` | `server_url` |
| API token | — | `PR_TOKEN` | — (stored in `credentials.json`) |
| Config file path | `--config` | — | — |

Precedence: `--server` > `PR_SERVER_URL` > config file. The config file is
`$XDG_CONFIG_HOME/page-report/config.yml` (falling back to
`~/.config/page-report/config.yml`); a missing file is fine, a `--config` path
that does not exist is an error. In containers and CI, set `PR_SERVER_URL` and
`PR_TOKEN`; no credentials file or interactive step is needed.

## Troubleshooting

| Symptom | Cause / fix |
|---|---|
| `error: unauthenticated: invalid token` + `hint: run page-report login first` | No token, or it was revoked, deleted or expired → user creates a new one in the dashboard and re-runs `page-report login`. Rotating a token invalidates the old secret immediately. |
| `credentials are from an older version that used device-flow login` | The stored file predates server-minted tokens → `page-report login` with a token from the dashboard. |
| `stdin is not a terminal: pass --token-stdin` | `login` was run non-interactively → pipe the token in with `--token-stdin`, or set `PR_TOKEN`. |
| `error: not_found` on `delete`/`get` | The id does not exist, or it belongs to another user — the two are indistinguishable on purpose. |
| `server URL required: pass --server, ...` | No `--server`, `PR_SERVER_URL`, or `server_url` in config. |
| `invalid server URL ...: must be an absolute http(s) URL` | Missing scheme or host in the configured URL. |
| `content exceeds max upload size of N bytes` | Over 5 MiB — drop embedded images or split the page. |
| `content must not be empty` | A 0-byte file was uploaded; check the write step. |
| `invalid duration "..." (want e.g. 30d or 720h)` | Bad `--older-than` value. |
| Page loads but is unstyled / charts missing | CSP blocked external assets — inline them. |
| Viewer gets `forbidden` | Their identity is not on the server allowlist; server-side fix, tell the user. |
| `this is a dev build, not a released binary` | `update` on a source build → reinstall with `install.sh` or pass `--force`. |
