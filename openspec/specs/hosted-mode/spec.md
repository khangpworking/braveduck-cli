# hosted-mode Specification

## Purpose
`braveduck serve --users DIR` runs braveduck for several people: each person logs in through an outside login service and gets their own complete board with their own data. Two login setups exist: Caddy with Authelia on a server, or Cloudflare Tunnel with Cloudflare Access from someone's own computer. This spec covers what the operator configures and what changes for users on a shared server.

## Requirements

### Requirement: R-HST01 One board per user, data never shared
Each user SHALL get a data dir of their own (`DIR/<user>/`: database, profile, CVs, backups, session key) and a complete single-user braveduck over it, with its own scheduler and its own per-run API token. A user's data dir SHALL be created the first time they log in, and every user who already has one SHALL be opened at start, so their scheduled work runs before they visit. One user MUST NOT be able to see or change another user's data.

#### Scenario: First visit
- **WHEN** a new user logs in for the first time
- **THEN** an empty board is created for them in their own folder

### Requirement: R-HST02 braveduck does no logins itself
braveduck MUST NOT handle passwords on a shared server. With a login proxy, it SHALL trust the user named in the user header (default `Remote-User`) only on requests that also carry the proxy's secret in `X-Braveduck-Proxy-Secret`; any other request MUST get 403. The secret MUST be at least 32 characters, and the server MUST refuse to start without either a secret (`--proxy-secret-file` or `BRAVEDUCK_PROXY_SECRET`) or Cloudflare Access settings.

#### Scenario: Bypassing the proxy
- **WHEN** a request reaches braveduck's port without the proxy secret
- **THEN** it gets 403 "braveduck only answers through its login proxy"

### Requirement: R-HST03 Cloudflare Access login
With `--access-team` (an `https://` team address) and `--access-aud`, braveduck SHALL check the token Cloudflare Access signed on each request (team keys, issuer, audience, expiry) and take the user from its email, lower-cased. A request without a valid token MUST get 401 and never reach a board, even from the same machine. The board's log-out link SHALL default to Access's own log-out page.

#### Scenario: Local request without Access
- **WHEN** someone on the computer itself requests `/` without an Access token
- **THEN** they get 401 "not logged in"

### Requirement: R-HST04 User names and capacity
A user name SHALL be lower-cased and MUST start with a letter or digit and contain at most 64 of letters, digits, `.`, `_`, `@` and `-`; any other name MUST get 403. The server SHALL hold at most `--max-users` boards (default 50), and a new user past that MUST get 503 saying the server has no room.

#### Scenario: Server full
- **WHEN** a 51st new user logs in on a server with the default limit
- **THEN** they get 503 and no board is created

### Requirement: R-HST05 Proxy headers never reach a board
Before a request reaches a user's board, braveduck SHALL remove the proxy secret, the user header and the Access token, and present it to the board as a localhost request. `GET /healthz` SHALL answer without a login.

#### Scenario: Health check
- **WHEN** the proxy calls `/healthz`
- **THEN** braveduck answers `{"status":"ok"}`

### Requirement: R-HST06 Agent keys
On a shared server only, a user SHALL be able to make a key for their AI agent on their board, of the form `<user>:<random>`; braveduck MUST store only its SHA-256 hash and show the key once. A new key SHALL replace the old one, and removing it SHALL stop it working. `/mcp` SHALL skip the login service and accept only a valid agent key (401 otherwise). With `--login-users-file`, a key MUST stop working once its user is disabled or removed there.

#### Scenario: Disabled user
- **WHEN** the operator sets `disabled: true` for a user in the users file
- **THEN** that user's agent key is refused at `/mcp`

#### Scenario: Key on a laptop
- **WHEN** a user on their own computer asks for an agent key
- **THEN** it is refused, pointing them to `braveduck mcp`

### Requirement: R-HST07 Window features stay on the owner's computer
On a shared server, job-site logins, assisted apply and reading CV files by path MUST be off, since their windows or files would be on the server. `--owner` SHALL name the user whose computer it runs on (login name or email); for that user they stay on.

#### Scenario: Owner on Cloudflare mode
- **WHEN** braveduck runs with `--owner me@example.com` and that user starts a TopCV login
- **THEN** the login window opens on the computer's screen

### Requirement: R-HST08 Per-user session keys
On a shared server, each user's saved sessions and pasted decision-model keys SHALL be sealed with a key file in that user's own folder, never the OS keychain or a shared passphrase.

#### Scenario: Owner's sessions
- **WHEN** the owner logs in to TopCV on a shared server
- **THEN** the session is sealed with the owner's own key file

### Requirement: R-HST09 Shared limits
On a shared server, built-in sources MUST NOT crawl more often than every 60 minutes; at most `--max-browsers` browsers (default 2) SHALL run at once for all users, and one user at most one at a time; each board SHALL hold at most `--max-jobs-per-user` jobs (default 5000, 0 = no cap); and decision-model calls on the server's key SHALL be capped by `--jev-daily-limit` (default 50 per user per day, R-RAT11).

#### Scenario: Browser cap
- **WHEN** three users' page fetches start at once with the default cap
- **THEN** two run and the third waits for a free browser

#### Scenario: Restart
- **WHEN** the shared server restarts with many users whose crawls are due
- **THEN** each user's scheduler starts at a random point in its first minute, so the crawls don't all start at once

### Requirement: R-HST10 Browsers in a separate service
When `BRAVEDUCK_BROWSER_SERVICE` is set, every browser (page fetches, CV PDFs) SHALL run in the browser service (`braveduck browser-service`) instead of beside users' data. The browser service MUST require a key file whose key is at least 32 characters.

#### Scenario: Missing key
- **WHEN** `braveduck browser-service` starts without `--key-file`
- **THEN** it refuses to start

### Requirement: R-HST11 Facebook scanner API
With `--scanner-addr`, braveduck SHALL open the scanner API on its own listener, which MUST require `--scanner-key-file` with a key of at least 32 characters and refuse requests without it (401). The scanner SHALL be able to list every followed group once (from boards with Facebook on, group not paused), report each group's name and visibility, and hand over posts, which each follower's board filters itself (R-CRW11). Bodies are limited to 8 MB.

#### Scenario: Wrong scanner key
- **WHEN** a request to the scanner API carries a wrong key
- **THEN** it gets 401 and nothing changes
