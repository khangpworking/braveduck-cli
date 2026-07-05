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

> Status: **early — design/architecture phase.** No usable code yet.
> See [`ARCHITECTURE.md`](./ARCHITECTURE.md) for the full build plan and
> [`docs/`](./docs) for the feature spec and UI reference.

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
