# Implementation Plan: CLI Download Feature

## Overview

Add a `download` command to the `pr` CLI to fetch and save HTML report files from the server by ID. This enables agents to programmatically retrieve previously uploaded plan pages.

## Requirements

- Fetch HTML content from server using existing `GetPage` RPC with `include_content=true`
- Write content to local file with optional name override
- Support both individual file and batch download workflows
- Output file path on success (for further processing)
- Preserve content-type handling (default: text/html)

## API Surface

```bash
pr download <id> [--output FILE] [--json]
```

### Flags:

- `--output FILE` (optional): save to this path; default: `<id>.html`
- `--json` (optional): output `{"path": "...", "id": "...", "title": "..."}`

### Examples:

```bash
pr download abc123
pr download abc123 --output my-plan.html
pr download abc123 --json | jq .path
```

## Implementation Tasks

### 1. Add download command to CLI **[HIGH PRIORITY]**

- **File:** `cmd/pr/main.go`
- Add `downloadCmd()` cobra.Command
- Parse `id` positional arg + `--output`, `--json` flags
- Call `PageService.GetPage(id, include_content=true)`
- Write bytes to file (default: `<id>.html`, or `--output` value)
- Output file path to stdout (plain or JSON)
- Register in `root.AddCommand(...)`

### 2. Handle errors gracefully **[MEDIUM PRIORITY]**

- Check file write permissions before RPC call
- Validate output path (prevent directory traversal)
- Preserve error messages from GetPage (not found, auth, etc.)

### 3. Document in AGENTS.md **[HIGH PRIORITY]**

- Add "Downloading a report" section with examples
- Mention use case (retrieve previous plans for reference)
- Show JSON output format

Note: This is **ALREADY DONE**. AGENTS.md already contains full download documentation with examples and troubleshooting (lines 67-101).

### 4. Test coverage **[MEDIUM PRIORITY]**

- Unit test: command flag parsing
- Integration: mock GetPage RPC response + file write
- Edge cases: missing id, write permission denied, invalid output path

## Files Modified

1. `cmd/pr/main.go` — add `downloadCmd()`, register in root
2. Tests (new or existing test file)

Note: AGENTS.md already has complete documentation; no changes needed there.

## Proto Changes

**None required** — `GetPage` RPC already supports `include_content` flag.

## Success Criteria

- ✅ `pr download <id>` saves HTML to `<id>.html`
- ✅ `pr download <id> --output custom.html` respects output path
- ✅ `pr download <id> --json` outputs valid JSON with path, id, title
- ✅ Errors (not found, auth) print to stderr with exit code 1
- ✅ AGENTS.md documents the feature with examples (already complete)

## Timeline

- Implementation: 30–45 min (command + error handling)
- Testing: 15–20 min
- Documentation: 0 min (already done in AGENTS.md)

## How to Use the Feature (as documented in AGENTS.md)

Once implemented, agents can:

```bash
# Retrieve a plan by ID, save to default filename
pr download abc123

# Save to custom path
pr download abc123 --output my-plan.html

# Get JSON metadata including file path
pr download abc123 --json

# Integrate into workflow
PLAN_ID=$(echo "$PLAN_URL" | grep -oP 'view/\K[^/?]+')
pr download --id "$PLAN_ID" --output retrieved-plan.html
```

The feature completes a publish-retrieve cycle: agents can generate plans, upload them via `pr upload`, and later retrieve them via `pr download` for reference or further processing.
