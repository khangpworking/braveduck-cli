# braveduck-cli

A local-first, single-binary job-hunt agent for the AI-CLI community.

One Go binary, three faces over a shared core:

- **`braveduck …`** — CLI commands (add / rate / apply / list a job pipeline)
- **`braveduck mcp`** — an MCP server, so **Claude Code / Codex / agy** can drive
  your job hunt as native tools
- **`braveduck serve`** — a local web board (kanban) served on `localhost`

Everything runs on **your own machine** against **your own logins** — no hosting,
no accounts, and no stored passwords. You authenticate with TopCV / VietnamWorks /
etc. directly in a real browser; braveduck only ever holds an encrypted session
locally on your disk.

> Status: **early.** The job pipeline, the web board, the MCP server, AI
> rating and job-site logins work; CV/draft generation, status sync and
> auto-apply are not wired yet. See [`ARCHITECTURE.md`](./ARCHITECTURE.md) for the build plan and
> [`docs/`](./docs) for the feature spec and UI reference.

## Quick start

Requires Go 1.25+.

```bash
go install github.com/khangpworking/braveduck-cli/cmd/braveduck@latest
braveduck add --title "Data Analyst" --company "Duck Co" --url https://www.topcv.vn/...
braveduck list
braveduck serve        # web board on http://127.0.0.1:8080
```

Data lives in `~/.braveduck/braveduck.db` (override with `--db` or `BRAVEDUCK_DB`).
`braveduck doctor` checks your setup.

### Use it from Claude Code (MCP)

```bash
claude mcp add braveduck -- braveduck mcp
```

Then ask Claude things like *"save this job: <url>"*, *"what's in my interview
lane?"* or *"move job 12 to applied"*. The server speaks MCP over stdio and
exposes the board as tools (`list_jobs`, `add_job`, `set_status`, `get_stats`,
…). Codex and other MCP clients work the same way: point them at
`braveduck mcp`.

### Rate jobs with your own AI

braveduck never calls an AI itself and stores no API keys. You describe
yourself once, and whatever AI you already use does the judging:

```bash
braveduck profile      # creates ~/.braveduck/profile.md — fill in what applies, any language
```

- **From an agent (MCP):** ask *"rate my unrated jobs"*. The agent calls
  `get_rating_queue`, judges each job against your profile and the rubric, and
  saves each verdict with `submit_rating`.
- **From any AI chat:** `braveduck rate` prints a ready-to-paste brief; the AI
  answers with lines like `braveduck rate 12 --score 4.2 --why "..."` that you
  run to save the scores.

Scores are 1.0-5.0 with letters A-F, and the reasons show on each card.
Rating stays off while the profile is still the blank template.

### Log in to job sites (sessions)

braveduck can act on job sites as you, with **no password ever stored**:

```bash
braveduck login topcv     # a browser window opens; you log in there yourself
braveduck platforms       # who you're logged in to
braveduck logout topcv    # delete the saved login (or: logout --all)
```

Built in: TopCV, VietnamWorks, JobOKO and CareerLink. Add any other site in
`~/.braveduck/platforms.json` (see `docs/specs/platform-sessions.md`). Sessions
are encrypted (AES-256-GCM) with a key from your OS keychain, a private key
file, or `BRAVEDUCK_PASSPHRASE`. Also on the board (🔑 Logins) and via MCP
(`list_platforms`, `connect_platform`, `disconnect_platform`).

Linux/WSL needs Chromium's libraries once: `sudo apt install -y libnss3 libasound2t64`.
Using a session to sync statuses or apply is a later step.

## Why

Job boards (TopCV, VietnamWorks, JobOKO, CareerLink…) are noisy and repetitive.
braveduck turns your search into a pipeline: discover, AI-rate against your
profile, tailor a CV, draft the outreach, and apply — driven either by its own
commands or by whatever AI CLI you already use, via MCP.

## Design principles

- **Local-first.** Your data and sessions live on your machine, in SQLite.
- **Never store passwords.** You log in on the real site; we keep an encrypted
  session, nothing more.
- **BYO agent.** The MCP server plugs into your existing Claude Code / Codex /
  agy — you don't have to adopt a new agent.
- **One binary.** `go install` and go.

## Roadmap

1. Core + CLI + local web board (single user, parity with the reference spec)
2. MCP server over the same core
3. Local browser (CDP) login + apply for TopCV / VNW / JobOKO / CareerLink
4. Community release

## License

MIT — see [LICENSE](./LICENSE).
