# braveduck-cli — Architecture

## As built (current)

The rest of this file is the original **build plan** (October 2026), kept
for its reasoning (login model, security, data at rest, API contract). Where
it names tools or splits work between builders, the code went another way;
this section is what exists.

**One binary, three faces over one core.** `cmd/braveduck` is the CLI (a
plain `switch` over subcommands, `flag` per command). `braveduck serve`
runs the board: `internal/api` (net/http `ServeMux`, the embedded board in
`internal/api/web`, a per-run token and a localhost-Host guard on every
request, SSE live updates). `braveduck mcp` runs `internal/mcp` (the
official `modelcontextprotocol/go-sdk`, stdio). All three call
**`internal/service`**, which holds every rule once (validation, duplicates,
the activity feed) and maps errors to `ErrNotFound` / `ErrInvalid` /
`ErrDuplicate` for each face.

**Storage.** `internal/store`: SQLite through `modernc.org/sqlite` (pure Go,
no cgo), hand-written SQL, numbered migrations tracked in `PRAGMA
user_version`. Files beside the database in the data dir (`~/.braveduck`,
0700): `profile.md`, the uploaded CV (`cv/`), tailored CVs (`cvs/`), the
session key and daily backups (`backups/`).

**Core packages** (`internal/core/*`, no I/O policy of their own):
`jobs` (expiry grammar and clocks), `keyword` (crawler filters), `rating`
(A/B rater config, verdicts), `scrape` (schema.org JobPosting, pasted text,
supported links, SSRF guard), `crawl` (VietnamWorks API, CareerLink),
`cvfile` (PDF/.docx/Markdown/HTML text), `platform` (job-site registry),
`vault` (AES-256-GCM sessions; key from the OS keychain, a key file or a
passphrase), `browser` (go-rod over a self-provisioned Chromium: login
windows, assisted-apply windows, PDF printing, page fetching), `topcv`
(read-only application history), `geo` (OSM Nominatim, cached),
`jev` (TypeSafe's Jev decision model), `apply`, `markdown`.

**Background work** (`service.RunScheduler`, while the board runs): built-in
crawlers on their schedules, TopCV status sync, Jev rating, an hourly sweep
that moves expired jobs to Closed, and a daily database backup. External
tools (fb-watcher) report their runs with `braveduck crawl-report`.

**The board** is plain HTML/CSS/JS with no build step, embedded with
`go:embed`; every job field is treated as hostile text (`esc`, `safeUrl`).

**Shared server** (`braveduck serve --users DIR`, `internal/hosted`): one
complete single-user braveduck per user (store, service with its scheduler,
the same board and API with its own per-run token) in `DIR/<user>/`. It
does no logins: Caddy asks Authelia about each request and passes the user
in `Remote-User`, which `internal/hosted` trusts only with the secret Caddy
adds (`X-Braveduck-Proxy-Secret`). In that mode the service refuses the
visible-window features (job-site login, assisted apply), seals sessions with
a key file per user (never the OS keychain or a shared passphrase), and all
users share a cap on running browsers. `/mcp` is each user's MCP server over
Streamable HTTP (stateless, cross-origin protected), without the tools that
read server files or open windows. With `BRAVEDUCK_BROWSER_SERVICE` set,
every browser runs in go-rod's browser service (its own container in
`deploy/hosted`) instead of beside the users' data. Setup and an end-to-end test of the
chain: `deploy/hosted`.

**Tests.** `go test ./...` (unit; CI adds `-race` and staticcheck) and
`go test -tags browser` (real Chromium: the board, logins, PDFs). Specs live
in `docs/` and `docs/specs/`.

---

# Original build plan (October 2026)

> Working name: **braveduck-cli**. A local-first, single-binary Go tool for the
> AI-job-hunt-agent community. Three faces over one core: CLI commands, an MCP
> server, and a locally-served web board. No hosting, no accounts, no stored
> passwords — everything runs on the user's own machine against their own logins.

This doc has three parts:
1. **Shared architecture** (read by both builders — this is the contract between them)
2. **Backend/CLI/MCP spec** → for **opencode**
3. **UI spec** → for **agy**

The two builders can work in parallel because Part 1 defines the REST API
contract they both build to. opencode implements it; agy's UI consumes it
(against a stub/mock server if the real one isn't ready yet).

---

## Part 1 — Shared Architecture

### 1.1 The three faces, one core

```
                    ┌─────────────────────┐
                    │   braveduck core     │   (Go packages, SQLite)
                    │ jobs / rating / apply │
                    │ / cv / scraping       │
                    └──────────┬──────────┘
              ┌────────────────┼────────────────┐
              │                │                │
      braveduck <cmd>   braveduck mcp     braveduck serve
      (CLI commands)    (MCP tools for    (embedded web UI,
                         Claude Code/      localhost, REST
                         Codex/agy)        API underneath)
```

- **One Go binary.** `go install` / brew / GitHub release. No server, no Docker
  required for a user to run it.
- **SQLite** (not Postgres) — local-first, one file, zero config. Lives at
  `~/.braveduck/braveduck.db` (path overridable via `BRAVEDUCK_HOME`).
- **The web UI's HTML/CSS/JS is embedded in the binary** via Go's `embed.FS` —
  `braveduck serve` needs no separate deploy step.
- **`braveduck mcp` and `braveduck serve`'s REST API are both thin adapters**
  over the same core package calls. No duplicated business logic.

### 1.2 Login model — "hold a session, never a password"

This is the part that makes local-first actually safe. Two supported modes:

**Mode A — Managed login (default)**
- `braveduck login topcv` (and `login vnw`, `login joboko`, `login careerlink`)
  opens a real, visible Chromium window to the platform's actual login page.
- The user logs in themselves — password, OTP, captcha, whatever the site
  requires. **braveduck never sees or stores the password.**
- On success, braveduck captures the resulting cookies/session and persists
  them **encrypted at rest** (see 1.4) in the local SQLite/`~/.braveduck/`
  data dir, keyed per platform.
- Later commands (`scrape`, `apply`, `status-sync`) reuse that session
  headlessly via CDP. When it expires, the user re-runs `login`.

**Mode B — Attach to the user's own running Chrome**
- Power-user path: connect via CDP to a Chrome the user is already logged
  into (same trick as `browser-harness`). Zero profile management, lowest
  bot-detection footprint, no separate encrypted-session storage needed for
  that platform (it's live in their own browser).

Both modes reuse the same downstream apply/scrape code — only how the
session is obtained differs.

**Explicitly rejected:** auto-filling stored username/password. It reintroduces
the exact liability local-first is meant to avoid, and breaks on OTP/captcha.

### 1.3 Local server security (non-optional, `braveduck serve` + `mcp`)

- Bind **`127.0.0.1` only** — never `0.0.0.0` — so nothing off-machine can
  reach it.
- Generate a **per-run token** on startup (printed to terminal / put in the
  URL braveduck opens), required on every request. Prevents another local
  process or a malicious webpage from hitting `localhost:PORT` (DNS-rebinding
  / CSRF-on-localhost class of bug).
- Same token model for the MCP server's local transport.

### 1.4 Data at rest

- Session cookies encrypted with a key derived from the OS keychain
  (`keyring` — macOS Keychain / Windows Credential Manager / Linux
  Secret Service), falling back to a passphrase-derived key if no keychain
  is available. Never plaintext session data on disk.
- Everything else (job listings, scores, notes) is plain SQLite — it's the
  user's own data on their own disk, no special handling needed.

### 1.5 REST API contract (the seam between opencode's backend and agy's UI)

`braveduck serve` exposes this JSON API on `127.0.0.1:<port>` (token via
`Authorization: Bearer <token>` header or `?token=` query param for simple
`fetch()` calls from the embedded UI). This is the **one thing both builders
must agree on and not diverge from independently** — if it needs to change,
update this doc first.

```
GET    /api/jobs                    list jobs (query: status, platform, sort)
GET    /api/jobs/:id                single job detail
POST   /api/jobs                    add job (paste JD or URL)
PATCH  /api/jobs/:id                update fields (status, pinned, keep, notes...)
DELETE /api/jobs/:id                delete
POST   /api/jobs/:id/rate           trigger AI scoring (body: {model?})
POST   /api/jobs/:id/rate/ask       ask an additional model for this job
POST   /api/jobs/:id/cv             generate/regenerate tailored CV
GET    /api/jobs/:id/cv.pdf         download the CV
POST   /api/jobs/:id/draft          draft/redraft application email
POST   /api/jobs/:id/draft/approve  approve + send the draft
POST   /api/jobs/:id/draft/reject
POST   /api/jobs/:id/apply          trigger the platform apply engine
POST   /api/jobs/:id/keep           exempt from auto-clear
POST   /api/jobs/:id/pin

GET    /api/stats                   funnel counts + interview-rate widget data
GET    /api/crawl-status            per-source last-run/health
POST   /api/crawl/:source/retry

GET    /api/platforms               connection status per platform (topcv/vnw/...)
POST   /api/platforms/:name/login   kick off Mode-A managed login (opens browser)
DELETE /api/platforms/:name/login   disconnect / clear session

GET    /api/rating-config           current default model + A/B config
PUT    /api/rating-config
GET    /api/keywords                crawler keyword lists
PUT    /api/keywords

GET    /api/events                  SSE stream (mirrors current live-update behavior)
GET    /api/activity                recent activity log (for the log strip)
```

Response shapes mirror the fields already enumerated in
`career-page-functional-spec.md` §11 (title, company, salary, status, score*,
expires_at, apply_state, cv_*, draft_*, discovery_source, company_*, etc.) —
reuse those field names verbatim so the UI work already validated against
mock data ports over directly.

---

## Part 2 — Backend / CLI / MCP (assign to **opencode**)

### 2.1 Stack

- **Go** 1.23+. `net/http` + `chi` for routing. No heavy framework.
- **SQLite** via `mattn/go-sqlite3` or `modernc.org/sqlite` (pure-Go, no cgo —
  prefer this for easier cross-compilation of release binaries).
- **`sqlc`** to generate typed Go from hand-written SQL — no ORM.
- **`goose`** or **`golang-migrate`** for migrations.
- **`chromedp`** for local Chrome/CDP control (login capture + scrape/apply) —
  same underlying protocol as `browser-harness`, just Go instead of Python.
- **MCP:** `mark3labs/mcp-go` (or the official Go MCP SDK if stable) to expose
  the tool list below.
- **Session encryption:** `99designs/keyring` (cross-platform OS keychain).

### 2.2 Core packages (rough shape)

```
/cmd/braveduck/          main.go — cobra CLI root, wires subcommands
/internal/core/
  jobs/                  CRUD, status transitions, expires_at grammar (port
                         directly from src/lib/jobExpiry.ts logic)
  rating/                multi-model scoring, A/B orchestration, dealbreakers
  cv/                    CV tailoring (calls out to an LLM), PDF render
  draft/                 email drafting + approve/send
  apply/                 per-platform apply engines (topcv, vnw, joboko,
                         careerlink) — port logic from the existing Python
                         apply skills, now driving chromedp instead
  session/               Mode A/B login capture, encrypted persistence
  scrape/                deadline + salary detail-page scraping (port
                         topcv_detail.py logic), public discovery (SerpAPI)
/internal/api/            REST handlers implementing the §1.5 contract
/internal/mcp/             MCP tool implementations (thin wrappers over core)
/internal/db/              sqlc-generated code + migrations
/web/                      embedded UI build output (agy's deliverable lands
                         here; go:embed picks it up)
```

### 2.3 SQLite schema (v1 — port of the current `job_listings` shape)

Carry over the existing field set (it's already battle-tested — see
functional-spec §11), adapted to SQLite conventions:

```sql
CREATE TABLE jobs (
  id                    INTEGER PRIMARY KEY AUTOINCREMENT,
  title                 TEXT NOT NULL,
  company               TEXT NOT NULL,
  url                   TEXT,
  location              TEXT,
  salary                TEXT,
  skills                TEXT,                 -- comma-separated, as today
  status                TEXT NOT NULL DEFAULT 'saved', -- saved/waiting/applied/interview/offer/closed
  pinned                INTEGER NOT NULL DEFAULT 0,
  keep                  INTEGER NOT NULL DEFAULT 0,
  notes                 TEXT,

  jd_summary            TEXT,
  jd_ai_summary         TEXT,
  jd_full               TEXT,

  discovery_source      TEXT,                 -- topcv/vietnamworks/joboko/careerlink/serpapi/linkedin/paste
  saved_at              TEXT NOT NULL,

  score                 REAL,
  score_letter          TEXT,
  score_status          TEXT,                 -- pending/scored/failed
  score_reasoning       TEXT,

  apply_method          TEXT,                 -- email/on-site
  apply_state           TEXT,                 -- queued/filling/cooldown/failed/manual
  apply_next_at         TEXT,
  apply_detail          TEXT,

  cv_path               TEXT,
  cv_status             TEXT,
  cv_version            INTEGER,
  cv_generated_at        TEXT,

  draft_email           TEXT,
  draft_status          TEXT,
  draft_notes           TEXT,
  contact_email         TEXT,

  expires_at            TEXT,                 -- grammar: NULL / 'unknown' / 'expire:ISO' / 'applied:ISO'
  followup_sent_at      TEXT,

  topcv_status          TEXT,
  vnw_status             TEXT,

  company_active_date   TEXT,
  company_distance_km   REAL,
  company_geo_status    TEXT,
  company_masothue_url  TEXT,
  company_masothue_name TEXT,

  created_at             TEXT NOT NULL,
  updated_at             TEXT NOT NULL
);

CREATE TABLE job_ratings (        -- per-model A/B ratings, one row per (job, model)
  job_id     INTEGER NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  model      TEXT NOT NULL,
  status     TEXT NOT NULL,       -- pending/scored/failed
  score      REAL,
  score_letter TEXT,
  score_reasoning TEXT,
  PRIMARY KEY (job_id, model)
);

CREATE TABLE platform_sessions (   -- encrypted session blobs, one per platform
  platform    TEXT PRIMARY KEY,   -- topcv/vietnamworks/joboko/careerlink
  session_enc BLOB NOT NULL,
  created_at  TEXT NOT NULL,
  expires_hint TEXT
);

CREATE TABLE crawl_runs (
  name TEXT PRIMARY KEY,
  last_success_date TEXT,
  last_run_at TEXT
);

CREATE TABLE keywords ( ... );     -- crawler keyword lists, per source
CREATE TABLE rating_config ( ... ); -- default model + A/B model set
```

### 2.4 CLI command surface (cobra)

```
braveduck init                          detect + wire Claude Code/Codex/agy MCP config
braveduck login <platform>              Mode A managed login
braveduck login <platform> --attach     Mode B: attach to running Chrome
braveduck logout <platform>

braveduck add <url|-->                  add a job (URL or piped JD text)
braveduck list [--status=] [--platform=]
braveduck show <id>
braveduck rate <id> [--model=]
braveduck cv <id>
braveduck draft <id>
braveduck apply <id>
braveduck status <id> <stage>

braveduck crawl [--source=]             run public-discovery crawlers now
braveduck sync <platform>               status-sync (recruiter views, closed jobs)

braveduck serve [--port=]               local web UI
braveduck mcp                           MCP server (stdio, for CLI-tool config)
braveduck doctor                        env/session/health check
```

### 2.5 MCP tool list (exposed by `braveduck mcp`)

Mirrors the REST API 1:1 so behavior is identical whether driven by a human
via the UI or an AI agent via MCP:

```
list_jobs, get_job, add_job, update_job, delete_job,
rate_job, ask_model, generate_cv, draft_email, approve_draft, reject_draft,
apply_job, pin_job, keep_job,
get_stats, get_crawl_status, retry_crawl,
list_platforms, connect_platform, disconnect_platform,
get_rating_config, set_rating_config,
get_keywords, set_keywords
```

Each tool's input/output schema should match the REST contract's request/
response bodies — don't invent a second shape.

### 2.6 Porting priorities (don't re-derive logic that's already correct)

Port these directly from the existing TypeScript/Python — the *logic* is
proven, only the language changes:
- `expires_at` grammar + `parseExpiry`/`expiryState`/`appliedState` (jobExpiry.ts)
- Dealbreaker detection (textDealbreakers/geoDealbreakers)
- Interview-rate / morale calculation (successRate)
- TopCV detail scraping (topcv_detail.py → chromedp)
- Apply engines per platform (the 4 existing Python skills → Go + chromedp)

### 2.7 Explicitly out of scope for v1

- No multi-user auth, no hosted mode, no Postgres.
- No auto-fill of platform passwords.
- n8n-based workflows (follow-up email, interview detector) — port the *logic*
  into Go jobs later; don't take a dependency on n8n in the OSS tool.

---

## Part 3 — UI (assign to **agy**)

### 3.1 What to build

A static frontend (plain HTML/CSS/vanilla JS, or `templ`+HTMX server-rendered
partials — either is fine, pick whichever fits how it'll be embedded) that:

- Reproduces the **Career kanban board** exactly as scoped in
  `career-page-functional-spec.md` (attached — every feature, badge, panel,
  and interaction is enumerated there; this is the source of truth for
  *what* to build).
- Uses the **Airtable-style visual design** already validated and approved —
  reference file: the final mockup at `agy-airtable-final.html` (sober
  editorial: white canvas, dark-ink type, near-black pill primary CTAs, white
  outlined secondaries, coral/dark-green/peach/navy signature accents used as
  tasteful badge/callout colors, 20px/bold column titles — that fix is already
  baked in, keep it).
- Talks to the backend **only** through the REST API defined in §1.5 above.
  Don't invent extra endpoints; if something's missing, flag it back rather
  than guessing a shape.
- Ships as static assets (`/web/dist` or similar) that opencode's Go binary
  will `go:embed`. No Node build step required *at runtime* — a build step to
  *produce* the static assets (Vite, esbuild, whatever) is fine, as long as
  the output is plain files.

### 3.2 Constraints (learned the hard way this session)

- **Do not drift into a marketing/landing-page aesthetic.** This is an
  internal dashboard/tool, not a SaaS homepage. No "Sign up free", no
  pricing sections, no testimonials.
- **Single responsibility per pass** — if asked for a targeted tweak (e.g.
  "make column titles bolder"), edit the existing files in place; don't
  regenerate the whole app from scratch as a side effect.
- Keep it framework-light if possible (plain HTML/CSS/JS embeds most simply);
  if a framework is used, the *build output* must still be static files with
  no runtime Node server.
- Real-time updates: implement against `GET /api/events` (SSE) the same way
  the current board does (see functional-spec §9) — badges update in place,
  no full-page reload on background changes.
- Local-only concerns (the `Authorization`/token requirement in §1.3) should
  be handled transparently — token is injected into the page by the Go
  server at serve-time (e.g. a small inline `<script>window.__TOKEN__=...`),
  not something the user manages.

### 3.3 Inputs to hand agy

1. `career-page-functional-spec.md` — full feature/purpose spec.
2. The Airtable mockup HTML (`agy-airtable-final.html`) — visual reference,
   titles-fixed version.
3. This doc's §1.5 (REST API contract) — the data contract to build against.
4. If the real backend isn't up yet, agy should build against a small mock
   JSON server or static fixtures matching the same shapes, then swap the
   base URL once opencode's API is live.

---

## Open decisions still worth a quick answer before kickoff

1. **Project name** — keep `braveduck-cli`, or rename now that it's a fresh
   public repo (clean break from the personal "braveduck" branding)?
2. **License** — MIT/Apache-2.0 for community adoption (matches Impeccable's
   choice)?
3. **Repo home** — new public GitHub repo, separate from the personal-assistant
   monorepo entirely (per your "completely separate web app" framing)?
