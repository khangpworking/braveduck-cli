# braveduck web board — build brief

You are building the local web board UI for **braveduck-cli**, a local-first,
single-binary Go job-board tool. This board is served by `braveduck serve` and
is embedded into the Go binary via `go:embed`. Your output ships inside the
binary, so it must be **buildless**.

## What to produce

Replace the placeholder file **`internal/api/web/index.html`**.

- **Buildless. No framework, no npm, no bundler, no build step.** Vanilla
  HTML + CSS + JS only. A single `index.html` is strongly preferred; if you
  must split, use plain `.css`/`.js` files in the same `internal/api/web/`
  folder referenced by relative paths (they are embedded together). Do NOT add
  package.json, node_modules, a src/ tree, or any tooling.
- No external network calls — no CDN scripts, no Google Fonts, no remote CSS.
  Use system font stacks. Everything must work fully offline.
- All data comes from the same-origin JSON API (below) via `fetch`.
- Dark theme by default (the tool is a terminal-adjacent dev tool). A light
  mode is nice-to-have, not required.
- HTML-escape every string that comes from the API before inserting into the
  DOM (the placeholder has an `esc()` helper — jobs contain user/JD text).

## The board

A **kanban** of six lanes, left to right, matching the `status` field:

`saved` → `waiting` → `applied` → `interview` → `offer` → `closed`

Each job is a card in its lane. Card shows: title, company, salary (if any),
a pin indicator when `pinned`, and a small "keep" indicator when `keep`.
Newest-first within a lane is fine (the API already returns newest-first).

### Interactions (all via the API)

1. **Move lane** — drag a card to another lane, OR a lane-picker on the card.
   `PATCH /api/jobs/{id}` with `{"status":"applied"}`. On success the API
   returns the updated job; re-render from that.
2. **Toggle pin** — `PATCH /api/jobs/{id}` with `{"pinned":true|false}`.
3. **Toggle keep** — `PATCH /api/jobs/{id}` with `{"keep":true|false}`.
4. **Job detail** — click a card to open a panel/modal showing the fuller
   record: url (as a link if present), location, skills, summary/ai_summary,
   full_jd, discovery_source, score_letter, expires_at, saved_at. Only show
   fields that are non-empty.

Keep it responsive-ish (usable on a laptop; lanes can scroll horizontally on
narrow screens). Pinned cards should stand out (e.g. a top border/badge).

## JSON API contract (same origin)

- `GET /api/jobs` → array of Job (newest first). Optional query params
  `?status=applied` and `?source=topcv` narrow it — you can ignore these and
  fetch all, then group client-side by `status`.
- `GET /api/jobs/{id}` → one Job. 404 if missing.
- `PATCH /api/jobs/{id}` → body may contain any of `status` (string),
  `pinned` (bool), `keep` (bool). Only the named fields change. Returns the
  updated Job.
- `GET /api/keywords` → the crawler filter lists (not needed for v1; ignore).
- `GET /healthz` → `{"status":"ok"}`.

### Job shape (snake_case JSON)

Fields you'll actually use for the board:

```
id (number)              title (string)          company (string)
url (string)             location (string)       status (string, the lane)
pinned (bool)            keep (bool)              notes (string)
salary (string)          skills (string, CSV)    summary (string)
ai_summary (string)      full_jd (string)        discovery_source (string)
score_letter (string)    score_status (string)   expires_at (string)
saved_at (string, RFC3339)   created_at   updated_at
```

Every other field exists but is empty for most jobs — don't rely on them.
Empty strings mean "unset"; `score` is 0 when unscored (use `score_letter`
being non-empty to decide whether to show a score).

## Live server for testing

A dev server with seeded data is running at **http://127.0.0.1:8091** while you
work. Hit it directly (`curl http://127.0.0.1:8091/api/jobs`) to see the real
shape, and open it in a browser to iterate. Do NOT hardcode that port — the
board must fetch from its own origin (relative URLs like `/api/jobs`), because
in production it's served from whatever `--addr` the user chose.

## Design

Use the **impeccable** design skill for polish. Aim for a clean, calm,
information-dense board — think Linear/Height, not a toy. The duck (🦆) is the
brand mark; "braveduck" is the wordmark. Ship a board that feels like a real
product, not a placeholder.

## Definition of done

- `internal/api/web/index.html` (+ optional sibling static assets) replaces the
  placeholder, is buildless, offline-capable, and renders the six-lane kanban.
- Move-lane, pin, keep, and detail-view all work against the API.
- No external requests, no build tooling added anywhere in the repo.
