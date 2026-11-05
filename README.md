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

> Status: **early, used daily.** Finding, rating, tailoring and applying all
> work for one user on their own machine. See [`ARCHITECTURE.md`](./ARCHITECTURE.md)
> for the design and [`docs/`](./docs) for the feature spec.

## What it does

- **Finds jobs.** Built-in crawlers for VietnamWorks and CareerLink run on a
  schedule from your search terms; paste a TopCV, VietnamWorks, JobOKO,
  CareerLink or Facebook link (or plain job text) to add one yourself.
  Duplicates are caught however a job arrives.
- **Rates them against you.** Upload your CV and say what you want; every job
  gets an A–F score with reasons — from Jev (TypeSafe's decision model) by
  default, or from your own AI (Claude Code, any chat).
- **Tells you what to do next.** *Today* lists the best matches not applied to
  yet, deadlines this week and applications to follow up; optional desktop
  notifications for new great matches.
- **Helps you apply.** Your AI tailors a CV (printed to PDF) and drafts the
  email; on any site you're logged in to (TopCV, VietnamWorks, JobOKO,
  CareerLink) a visible window opens on the job with your login and you press
  submit — braveduck never submits for you. Your TopCV application statuses
  sync back to the board.

## Quick start

Requires Go 1.25+.

```bash
go install github.com/khangpworking/braveduck-cli/cmd/braveduck@latest
braveduck cv upload ~/Downloads/my-cv.pdf   # .pdf .docx .md .html — fills your profile, kept for applying
braveduck add https://www.topcv.vn/viec-lam/...   # TopCV, VietnamWorks, JobOKO, CareerLink
braveduck add https://www.facebook.com/groups/.../posts/.../ - < post.txt   # Facebook: link + post text
braveduck keywords add search_queries "n8n" "marketing ai"   # what the crawlers search for
braveduck crawl        # fetch new jobs now (they also run on a schedule while the board is up)
braveduck today        # what to act on now
braveduck list --search "ky su ai"   # accents optional
braveduck export --out jobs.csv      # every job as a spreadsheet (--json for full records)
braveduck serve        # web board on http://127.0.0.1:8080
```

A new board walks you through the same steps (**Get started**: CV → what
you're looking for → search terms). Everything else is under **⚙ Settings**.
On the board, `/` searches, `n` adds a job, Esc closes a window and `?` lists
the shortcuts.

Data lives in `~/.braveduck/braveduck.db` (override with `--db` or `BRAVEDUCK_DB`).
`braveduck doctor` checks your setup (including whether the board is running).
While the board runs it keeps a daily
copy of the database for a week in `~/.braveduck/backups/`; `braveduck backup`
makes one now. To restore, stop the board and copy a backup over
`braveduck.db`.

Crawls, rating, the expiry sweep and backups only run while the board does.
To keep it running on Linux, a systemd user service works:

```ini
# ~/.config/systemd/user/braveduck.service
[Service]
ExecStart=%h/go/bin/braveduck serve --no-open
Restart=on-failure

[Install]
WantedBy=default.target
```

then `systemctl --user enable --now braveduck`.

### Use it from Claude Code (MCP)

```bash
claude mcp add braveduck -- braveduck mcp
```

Then ask Claude things like *"save this job: <url>"*, *"what's in my interview
lane?"* or *"move job 12 to applied"*. The server speaks MCP over stdio and
exposes the board as tools (`list_jobs`, `add_job`, `set_status`, `get_stats`,
…). Codex and other MCP clients work the same way: point them at
`braveduck mcp`.

### How jobs are rated

Rating compares each job with your profile (`~/.braveduck/profile.md`, also
editable on the board under 👤 Profile). `braveduck cv upload` fills its
"About me" part from your CV; add your target roles, minimum pay, location and
deal-breakers. Rating stays off while the profile is empty.

- **Jev (default).** braveduck sends your profile and the job's text to
  TypeSafe's Jev decision model (`api.typesafe.ai`) and stores the verdict —
  every 30 minutes for new jobs while the board runs. Key in
  `~/.config/jev-route/typesafe.env`; `braveduck jev status` shows what is
  sent. See `docs/specs/jev-rating.md`.
- **Your own AI.** From an agent over MCP, ask *"rate my unrated jobs"*
  (`get_rating_queue` → `submit_rating`). From any chat, `braveduck rate`
  prints a ready-to-paste brief and the AI answers with
  `braveduck rate 12 --score 4.2 --why "..."` lines. Claude can run as an A/B
  challenger next to Jev (`braveduck rate-config`).

Scores are 1.0–5.0 with letters A–F; the reasons show on each card. Tailored
CVs and emails are written the same bring-your-own-AI way (`braveduck cv <id>`,
`braveduck draft <id>`): braveduck stores what your AI writes and never sends
email itself.

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
file, or `BRAVEDUCK_PASSPHRASE`. Also on the board (⚙ Settings → 🔑 Job-site logins) and via MCP
(`list_platforms`, `connect_platform`, `disconnect_platform`).

Linux/WSL needs Chromium's libraries once: `sudo apt install -y libnss3 libasound2t64`.

`braveduck apply <id>` (or **Apply on …** on the card) opens the job in a
visible window with that site's login and your CV ready — you review and
submit yourself. On TopCV, braveduck then checks your application history;
on other sites it asks whether you submitted. With a TopCV login,
`braveduck sync topcv` (also every 6 h) moves the jobs you applied to on
TopCV to Applied.

### Run it for several people

`braveduck serve --users DIR` is a shared server: everyone logs in and gets
their own board, with their own data. Logins are handled by
[Authelia](https://www.authelia.com) behind [Caddy](https://caddyserver.com)
(HTTPS from Let's Encrypt); [deploy/hosted](deploy/hosted/README.md) has the
Docker Compose setup, scripts to add people, and a test of the whole chain.
AI agents connect to `https://…/mcp` with a key made on the person's board. Job-site
logins and assisted apply open windows on your own screen, so they stay on
braveduck running on your computer.

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
- **You stay in control.** braveduck never submits an application, never
  sends an email, and only crawls what job sites' robots.txt allows.
- **One binary.** `go install` and go.

## Roadmap

1. ~~Core + CLI + local web board~~ ✓
2. ~~MCP server over the same core~~ ✓
3. ~~Logins, status sync and assisted apply~~ ✓ (status sync: TopCV only so far)
4. Community release

## License

MIT — see [LICENSE](./LICENSE).
