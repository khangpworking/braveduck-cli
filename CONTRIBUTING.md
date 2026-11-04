# Contributing

## Layout

braveduck is one Go binary with three faces over one core:

- `internal/service`: every operation, implemented once (validation, duplicate
  checks, the activity feed). New behaviour goes here.
- `cmd/braveduck` (CLI), `internal/api` (REST + the embedded board in
  `internal/api/web`), `internal/mcp` (MCP tools): thin adapters that only
  translate input/output and map service errors.
- `internal/store`: SQLite. Schema changes are **new entries** appended to
  `migrations` in `store.go`. Never edit a shipped migration.
- `internal/core/*`: pure logic plus the browser, vault and platform registry.

Design specs live in `docs/specs/`. If a change alters a spec, update the spec
in the same PR.

## Workflow

Branch from `main` → PR → squash-merge. CI must be green.

## Checks (what CI runs)

```bash
gofmt -l .                                   # must print nothing
go vet ./... && go vet -tags browser ./...
go mod tidy && git diff --exit-code go.mod go.sum
for f in internal/api/web/*.js; do node --check "$f"; done   # board JS still parses
go test -race ./...                           # unit tests (race needs cgo)
```

Real-browser tests drive Chromium against a fake job site and the embedded
board:

```bash
CLOAKBROWSER_BINARY_PATH=/path/to/chrome go test -tags browser ./internal/core/browser/ ./internal/service/ ./internal/api/
```

On minimal Linux or WSL, Chromium needs `libnss3` and `libasound2`
(`sudo apt install -y libnss3 libasound2t64`).

Writing board tests:

- Put a new feature's tests in its own `internal/api/*_browser_test.go` file
  (with `//go:build browser`). Two PRs that both append to
  `board_browser_test.go` conflict.
- Click with `clickID(page, id)` or `el.MustEval("() => this.click()")`.
  rod's pointer `MustClick` waits for the element to be visible, stable and
  uncovered, and a toast or a reflow can stall it for the full timeout. Only
  tests that check hit-testing itself (the narrow-viewport test) use real
  pointer clicks.
- Every button needs a spoken name: text with letters, or an `aria-label`
  (`TestBoardButtonsHaveNames`).
- Wait for state with `waitUntil` or rod's `MustElement*`, never `time.Sleep`.
  On WSL the suite sometimes freezes in a random test; CI's Linux runners are
  the judge.

## Rules that matter

- **Never store or log secrets.** Session data only leaves `internal/service`
  into the vault. Tests assert it never appears in API, MCP or log output.
- **Treat scraped job text as hostile.** Anything the board renders through
  `innerHTML` goes through `esc()` / `jsArg()` / `safeUrl()`.
- **Fetch only public URLs** (`scrape.CheckPublicURL`, `publicClient`).
- Stay on the Go version in `go.mod`: pin dependency versions that need a
  newer Go instead of letting `go get @latest` raise it.
