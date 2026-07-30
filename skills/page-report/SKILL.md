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
  printed page links live on a separate **pages** domain.
- **Logged in** — see below. Credentials are stored in
  `~/.config/page-report/credentials.json` (mode 0600) and persist across
  sessions.

## One-time login

Authentication uses an RFC 8628 OAuth device flow:

```sh
page-report login
```

It prints a verification URL and a code, then blocks:

```
Open https://github.com/login/device and enter code: ABCD-1234
Waiting for authorization...
```

**Relay the URL and code to the user verbatim and wait** — the user must
approve in their browser; the command polls until they do. Prefer having the
user run `page-report login` themselves rather than running it from an
unattended agent loop, since it cannot complete without them.

Repeat login only when a command fails with an authentication error.
`page-report logout` deletes the stored credentials.

## Publishing a report

```sh
page-report upload report.html --title "Weekly metrics"
# → https://pages.example.org/p/5gQG6fZu3S73
```

On success stdout is a single line: the shareable URL. **Always show this URL
to the user** — it is the deliverable. Never hand-build a page URL from the
configured server URL; that is the app domain, not the pages domain.

- `--title` defaults to the filename without extension, which is usually ugly.
  Always pass a real human title; it is what `list` and the page header show.
- `--json` prints `{"id": "...", "url": "..."}` — use it when you need the id
  for a later `delete`.
- Non-zero exit with `error: ...` on stderr means nothing was uploaded.

**Uploads are immutable.** There is no update command; a revision is a new
upload with a new URL. Pages are readable by everyone on the server allowlist,
so do not upload secrets, tokens, or third-party-confidential detail.

### Writing the HTML

Pages are served under a strict CSP:

```
default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; frame-ancestors 'none'
```

The file must therefore be **fully self-contained**:

- Inline `<style>` and `<script>` are allowed (`unsafe-inline`).
- External CSS/JS/fonts/images from any CDN are **blocked** — no Tailwind CDN,
  no Google Fonts, no chart.js from unpkg. Inline the library or skip it.
- Images: `data:` URIs only; inline SVG is fine.
- `fetch`/XHR to other hosts is blocked, and the page cannot be framed.
- Size cap: 5 MiB by default (`max_upload_bytes` server config). Empty files
  are rejected.
- Write a complete document — `<!doctype html>`, `<head>`, `<title>`,
  `<meta name="viewport">`. Nothing wraps it for you. Style it for light and
  dark, mobile width included.

`templates/report-template.html` in this skill is a CSP-safe starting point.

## Command reference

| Command | What it does |
|---|---|
| `page-report login` | Device-flow OAuth; stores credentials. Blocks on user approval. |
| `page-report logout` | Deletes stored credentials. |
| `page-report upload <file.html> [--title T] [--json]` | Publishes a page; prints its URL. |
| `page-report list [--json]` | Every page on the server, newest first. |
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
and are **not scoped to you** — every allowlisted user sees and can delete
every page, so `prune` can wipe other people's work. **Ask the user first**,
naming the ids and titles you are about to remove. When replacing an earlier
revision, upload the new page first, then ask whether to delete the old one.

## Patterns

- **Initial setup** — `page-report version` to confirm the binary, set
  `server_url` in the config file once, then `page-report login`.
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
| Server base URL (app domain) | `--server` | `PR_SERVER_URL` | `server_url` |
| Config file path | `--config` | — | — |

Precedence: `--server` > `PR_SERVER_URL` > config file. The config file is
`$XDG_CONFIG_HOME/page-report/config.yml` (falling back to
`~/.config/page-report/config.yml`); a missing file is fine, a `--config` path
that does not exist is an error. In containers and CI, set `PR_SERVER_URL` and
mount a pre-authenticated `credentials.json` — there is no non-interactive
login.

## Troubleshooting

| Symptom | Cause / fix |
|---|---|
| `error: unauthenticated` + `hint: run page-report login first` | No credentials or expired token → user runs `page-report login`. |
| `token expired and no refresh token stored` | Same; re-login required. |
| `server URL required: pass --server, ...` | No `--server`, `PR_SERVER_URL`, or `server_url` in config. |
| `invalid server URL ...: must be an absolute http(s) URL` | Missing scheme or host in the configured URL. |
| `content exceeds max upload size of N bytes` | Over 5 MiB — drop embedded images or split the page. |
| `content must not be empty` | A 0-byte file was uploaded; check the write step. |
| `invalid duration "..." (want e.g. 30d or 720h)` | Bad `--older-than` value. |
| Page loads but is unstyled / charts missing | CSP blocked external assets — inline them. |
| Viewer gets `forbidden` | Their identity is not on the server allowlist; server-side fix, tell the user. |
| `this is a dev build, not a released binary` | `update` on a source build → reinstall with `install.sh` or pass `--force`. |
