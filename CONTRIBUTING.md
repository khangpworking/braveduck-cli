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
node --check internal/api/web/app.js          # board JS still parses
go test -race ./...                           # unit tests (race needs cgo)
```

Real-browser tests drive Chromium against a fake job site and the embedded
board:

```bash
CLOAKBROWSER_BINARY_PATH=/path/to/chrome go test -tags browser ./internal/core/browser/ ./internal/service/ ./internal/api/
```

On minimal Linux or WSL, Chromium needs `libnss3` and `libasound2`
(`sudo apt install -y libnss3 libasound2t64`).

## Rules that matter

- **Never store or log secrets.** Session data only leaves `internal/service`
  into the vault. Tests assert it never appears in API, MCP or log output.
- **Treat scraped job text as hostile.** Anything the board renders through
  `innerHTML` goes through `esc()` / `jsArg()` / `safeUrl()`.
- **Fetch only public URLs** (`scrape.CheckPublicURL`, `publicClient`).
- Stay on the Go version in `go.mod`: pin dependency versions that need a
  newer Go instead of letting `go get @latest` raise it.
