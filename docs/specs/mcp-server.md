# Spec: `braveduck mcp` — MCP server

Status: **built** (`internal/mcp`; also implements `set_rating_config` and `get_stats`) · Owner: backend pipeline · Reviewer: audit gate
Depends on: nothing (wraps code that already works)

## Goal

Expose braveduck's core as an MCP server over **stdio**, so Claude Code / Codex /
agy can drive the job pipeline as native tools. Same behavior as the REST API and
the CLI — three faces, one core. No new business logic in this task: the read and
pipeline tools wrap handlers that already exist (`internal/api/api.go`,
`internal/store`). The intelligence tools (`rate_job`, `generate_cv`,
`draft_email`) are wired as tool *surfaces* here but their bodies land in the
separate rate/cv/draft task — until then they return a typed "not wired" error,
exactly like the CLI stubs.

## SDK

Use the official `github.com/modelcontextprotocol/go-sdk` (stdio transport). One
binary, no extra runtime. Register under a new `cmd/braveduck` case `mcp` calling
`internal/mcp.Serve(ctx, st)`.

## Transport & lifecycle

- `braveduck mcp` → stdio server, blocks until EOF/SIGINT.
- Reuses the same `*store.Store` the CLI/serve open (respects `--db` / `BRAVEDUCK_DB`).
- Structured logging to **stderr only** (stdout is the MCP channel — never print to it).

## Tools (v1)

Mirror the REST contract 1:1 — do not invent a second request/response shape.
Reuse the same JSON structs the REST handlers marshal.

### Wraps existing, working code — build all of these now
| tool | maps to | notes |
|------|---------|-------|
| `list_jobs` | `GET /api/jobs` | optional `status`, `platform` filters |
| `get_job` | `GET /api/jobs/{id}` | |
| `add_job` | `POST /api/jobs` | supported link (TopCV, VietnamWorks, JobOKO, CareerLink fetched; Facebook link + pasted post text, never fetched) or pasted JD; runs the no-AI extractor + built-in browser we shipped |
| `update_job` | `PATCH /api/jobs/{id}` | |
| `delete_job` | `DELETE /api/jobs/{id}` | |
| `set_status` | `PATCH /api/jobs/{id}` | thin alias: stage transition |
| `pin_job` / `keep_job` | `PATCH /api/jobs/{id}` | |
| `get_keywords` | `GET /api/keywords` | |
| `get_rating_config` | `GET /api/rating-config` | |
| `get_stats` | derive from store | interview-rate / morale (logic already ported) |
| `get_crawl_status` | `GET /api/crawl-status` | |
| `get_activity` | `GET /api/activity` | |

### Surface now, body later (return typed "not wired in this build yet")
`rate_job`, `ask_model`, `generate_cv`, `draft_email`, `approve_draft`,
`reject_draft`, `apply_job`, `crawl`, `retry_crawl`, `connect_platform`,
`disconnect_platform`, `set_rating_config`, `set_keywords`.

Rationale for surfacing early: a connected agent can discover the full tool list
and reason about the pipeline; each stub fails honestly rather than silently.

## Refactor note (do this cleanly, keep it surgical)

The REST handlers currently couple logic to `http.ResponseWriter`. Pull the
request→result core of each into a plain method on a shared service type
(`internal/core` or a small `internal/service`) that BOTH the HTTP handler and the
MCP tool call. Don't duplicate the logic into the MCP layer. If that refactor is
bigger than expected, stop and flag it — do not fork the behavior.

## Out of scope

- `braveduck init` (auto-writing agents' MCP config) — separate task.
- Any LLM call — that's the rate/cv/draft task.
- Auth/session/apply — parked.

## Done when

- `braveduck mcp` starts, a client (Claude Code) lists all tools.
- The "wraps existing" tools round-trip against a real DB (add a URL → it appears
  in `list_jobs` with extracted fields; status transitions persist).
- Surfaced-but-unbuilt tools return a typed not-wired error, no panic.
- stdout carries only MCP frames; logs go to stderr.
- `go vet ./...` clean; a smoke test drives add_job → list_jobs.
