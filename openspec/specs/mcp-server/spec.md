# mcp-server Specification

## Purpose
braveduck as tools for the user's own AI agent (Claude Code, Codex, agy and other MCP clients). `braveduck mcp` serves the job pipeline over stdio on the user's computer; on a shared server each user's agent reaches the same tools over HTTPS with a key. The tools are a thin face over the same core as the CLI and the board.

## Requirements

### Requirement: R-MCP01 Local stdio server
`braveduck mcp` SHALL run an MCP server over stdio until the client closes it or the process is interrupted, using the same database as the CLI (`--db`, `BRAVEDUCK_DB`). Nothing but MCP messages MUST be written to stdout; logs SHALL go to stderr.

#### Scenario: Connect Claude Code
- **WHEN** the user runs `claude mcp add braveduck -- braveduck mcp`
- **THEN** Claude Code lists braveduck's tools and can call them

### Requirement: R-MCP02 Same rules as every other face
Every tool SHALL call the same core operations as the CLI and the REST API, and return the same JSON shapes and the same validation errors. A not-found error SHALL suggest checking the id with `list_jobs`, and a duplicate error SHALL suggest finding the existing job with `list_jobs`.

#### Scenario: Duplicate via MCP
- **WHEN** an agent calls `add_job` with a link already on the board
- **THEN** the tool fails with the duplicate error and a hint to use `list_jobs`

### Requirement: R-MCP03 Tool set
The server SHALL offer tools to read the board, change jobs, rate, write CVs and drafts, find jobs, and act on job sites, as listed in the scenario below. `ask_model` SHALL be listed but MUST always fail with "not wired in this build yet". Each tool SHALL carry read-only, idempotent or destructive hints.

#### Scenario: Full tool list on the user's computer
- **WHEN** a client lists the tools of `braveduck mcp`
- **THEN** it gets the reads `list_jobs` (lane, source and accent-insensitive query filters), `get_job`, `get_stats`, `get_setup`, `get_today`, `get_activity`, `get_crawl_status`, `get_keywords`, `get_rating_config`, `get_profile`, `get_location`, `list_platforms`, `list_facebook_groups`, `get_crawl_schedule`
- **AND** the job changes `add_job`, `update_job`, `set_status`, `mark_followed_up`, `pin_job`, `keep_job`, `delete_job`
- **AND** the rating tools `get_rating_queue`, `rate_job`, `submit_rating`, `rate_with_jev`, `set_rating_config`
- **AND** the document tools `cv_brief`, `submit_cv`, `draft_brief`, `submit_draft`, `reject_draft`, `upload_cv`, `update_profile`
- **AND** the discovery tools `crawl`, `retry_crawl`, `set_crawl_schedule`, `set_keywords`, `follow_facebook_group`, `unfollow_facebook_group`
- **AND** the site tools `connect_platform`, `disconnect_platform`, `sync_platform`, `apply_job`, `confirm_applied`, `set_location`, plus the stub `ask_model`

#### Scenario: Stub tool
- **WHEN** an agent calls `ask_model`
- **THEN** it fails with "not wired in this build yet" and nothing changes

### Requirement: R-MCP04 Long actions return at once
Tools that wait on a person (`connect_platform`, `apply_job`) SHALL return at once: `connect_platform` with status connecting, `apply_job` with the site, the CV file to upload and the cover letter. Their outcome SHALL appear in `list_platforms` or `get_activity`, and a non-TopCV apply SHALL wait for `confirm_applied` with what the user said.

#### Scenario: Apply via agent
- **WHEN** an agent calls `apply_job` for a TopCV job
- **THEN** the tool answers with the CV path while the window stays open for the user

### Requirement: R-MCP05 Rating through the agent
An agent SHALL rate by calling `get_rating_queue` (or `rate_job` for one job), judging each job against the returned profile and rubric, and calling `submit_rating` once per job. A verdict with an empty model SHALL count as the default rater's and own the badge; an agent rating as an A/B challenger passes its own model id. While the profile is empty these tools MUST fail and the agent SHALL tell the user which file to fill in rather than invent a profile.

#### Scenario: Unrated jobs
- **WHEN** an agent is asked to "rate my unrated jobs"
- **THEN** it reads the queue and submits one verdict per job

### Requirement: R-MCP06 No secrets in tool output
No tool MUST return session data, cookie values, decision-model keys or agent keys; `list_platforms` SHALL return status only.

#### Scenario: Platform status
- **WHEN** an agent calls `list_platforms` after a TopCV login
- **THEN** the answer shows TopCV as connected with no session data

### Requirement: R-MCP07 Each user's MCP server on a shared server
On a shared server, each user's tools SHALL answer at `/mcp` over Streamable HTTP, stateless with JSON responses, refusing cross-origin browser requests, for the agent key that user made on their board (R-HST06). Tools that read a file on the server or open a window there (`upload_cv`, `connect_platform`, `apply_job`, `confirm_applied`) MUST be left out, except for the owner of the computer it runs on; the shortest crawl interval offered SHALL be the shared server's.

#### Scenario: Shared-server tool list
- **WHEN** an agent with a non-owner user's key lists the tools at `/mcp`
- **THEN** `upload_cv`, `connect_platform`, `apply_job` and `confirm_applied` are absent
