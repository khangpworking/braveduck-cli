# Contributing

## Layout

braveduck is one Go binary with three faces over one core:

- `internal/service`: every operation, implemented once (validation, duplicate
  checks, the activity feed). New behaviour goes here.
- `cmd/braveduck` (CLI), `internal/api` (REST + the embedded board),
  `internal/mcp` (MCP tools): thin adapters that only
  translate input/output and map service errors.
- `internal/store`: SQLite. Schema changes are **new entries** appended to
  `migrations` in `store.go`. Never edit a shipped migration.
- `internal/core/*`: pure logic plus the browser, vault and platform registry.
- `web/`: the board (React, TypeScript, Tailwind and shadcn/ui components).
  It talks only to the REST API. `npm run build` writes it to
  `internal/api/app`, which is committed: the Go binary embeds it and serves
  it at `/`, so building braveduck needs no Node.

Design specs live in `docs/specs/`. If a change alters a spec, update the spec
in the same PR.

## Workflow

Branch from `main` → PR → squash-merge. CI must be green.

## Checks (what CI runs)

```bash
gofmt -l .                                   # must print nothing
go vet ./... && go vet -tags browser ./...
go mod tidy && git diff --exit-code go.mod go.sum
go test -race ./...                           # unit tests (race needs cgo)
```

The board (Node 22):

```bash
cd web && npm ci
npm run typecheck && npm run lint && npm test   # types, oxlint, unit tests (vitest)
npm run build                                   # then commit internal/api/app with the change
npm run dev   # live-reloading board against a running braveduck (BRAVEDUCK_URL, default :8097)
```

CI rebuilds it and fails when the committed `internal/api/app` doesn't match
the source. Text on the board is written in English and translated in
`web/src/locales/vi.json`; `src/lib/i18n.test.ts` fails on a missing or
unused translation. Use `t("…")` in components and `N_("…")` for text kept in
data and translated where it's shown.

Real-browser tests drive Chromium against a fake job site and the embedded
board:

```bash
CLOAKBROWSER_BINARY_PATH=/path/to/chrome go test -tags browser ./internal/core/browser/ ./internal/service/ ./internal/api/
```

On minimal Linux or WSL, Chromium needs `libnss3` and `libasound2`
(`sudo apt install -y libnss3 libasound2t64`).

Writing board tests:

- Put a new feature's tests in its own `internal/api/app_*_browser_test.go`
  file (with `//go:build browser`); shared helpers are in
  `browser_helpers_test.go` and `app_browser_test.go` (`appBoard`, `appPage`).
- Find controls by what the user sees: `page.MustElementR("button", "Add job")`,
  `aria-label`s, roles. Click with `el.MustEval("() => this.click()")`: rod's
  pointer `MustClick` waits for the element to be stable and uncovered, and a
  toast or an animation can stall it. Radix menus open on pointer down, so
  dispatch a `PointerEvent("pointerdown")` on their trigger.
- Every control needs a spoken name: text with letters, or an `aria-label`
  (`TestAppButtonsHaveNames`).
- Wait for state with `waitUntil` or rod's `MustElement*`, never `time.Sleep`.
  On WSL the suite sometimes freezes in a random test; CI's Linux runners are
  the judge.

## Rules that matter

- **Never store or log secrets.** Session data only leaves `internal/service`
  into the vault. Tests assert it never appears in API, MCP or log output.
- **Treat scraped job text as hostile.** The board renders it as React text,
  never as HTML (no `dangerouslySetInnerHTML`), and only `http(s)` posting
  links become links (`postingUrl`).
- **Fetch only public URLs** (`scrape.CheckPublicURL`, `publicClient`).
- Stay on the Go version in `go.mod`: pin dependency versions that need a
  newer Go instead of letting `go get @latest` raise it.
