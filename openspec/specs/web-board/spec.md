# web-board Specification

## Purpose
The local web board that `braveduck serve` runs: a React app built into the binary, served on this machine only, that shows the jobs of each lane as a list with the chosen job beside it, Today, and Settings. It talks only to braveduck's own JSON API and stays safe against other web pages and hostile job text.

## Requirements

### Requirement: R-WEB01 The board listens on this machine only
`braveduck serve` MUST only listen on a loopback address and SHALL refuse any other `--addr`, pointing to the shared-server mode instead. The default address SHALL be `127.0.0.1:8080`, and the board SHALL open the user's browser unless `--no-open` is given.

#### Scenario: Public address
- **WHEN** the user runs `braveduck serve --addr 0.0.0.0:8080`
- **THEN** it refuses to start and mentions `serve --users`

### Requirement: R-WEB02 A per-run token on every API call
Each run SHALL make a new random token, put it into the board page it serves, and require it as `Authorization: Bearer <token>` on every `/api/` request; only the live-update stream MAY carry it as a `token` query parameter. The board page MUST NOT be cached, so a restart never leaves a page with a dead token.

#### Scenario: Request without the token
- **WHEN** a local process calls `GET /api/jobs` without the token
- **THEN** it gets 401 "missing or bad token — reload the board"

### Requirement: R-WEB03 Localhost Host header and no framing
Every request whose Host header does not name localhost or a loopback address MUST be refused with 403. Every answer SHALL tell browsers not to frame the board, not to guess content types, and not to send a referrer.

#### Scenario: DNS rebinding
- **WHEN** a request arrives with Host `evil.example:8080`
- **THEN** it gets 403 "braveduck only answers on localhost"

### Requirement: R-WEB04 Live updates
The board SHALL get live updates over a server-sent event stream: while at least one board listens, braveduck checks the database once a second and tells every open board when something changed, and sends a keep-alive every 25 seconds.

#### Scenario: Change from the CLI
- **WHEN** the user moves a job with the CLI while the board is open
- **THEN** the open board updates without a reload

### Requirement: R-WEB05 Job text is treated as hostile
The board MUST render every job field as text, never as HTML, and only `http` or `https` posting links SHALL become links.

#### Scenario: Script in a job description
- **WHEN** a job's description contains `<script>` markup
- **THEN** the board shows it as plain text

### Requirement: R-WEB06 Language
Board text SHALL be written in English and translated into Vietnamese, and every English text MUST have a translation. The board's language SHALL be Vietnamese unless `BRAVEDUCK_LANG=en` is set, and a language the user picks in Settings SHALL be remembered in that browser.

#### Scenario: Default language
- **WHEN** the board is opened with no `BRAVEDUCK_LANG` and no saved choice
- **THEN** it is shown in Vietnamese

### Requirement: R-WEB07 Lanes, lists and sorting
The board SHALL show one lane at a time as a list, with the chosen job beside it (on a phone, one at a time), plus a Today view. A list SHALL sort by best fit (default), newest, highest pay, nearest or closing soonest, with pinned jobs always first. The user MAY drag the divider between the list and the job to resize them.

#### Scenario: Sort by pay
- **WHEN** the user picks "Highest pay" in the Saved lane
- **THEN** pinned jobs come first, then jobs by the top of their pay range

### Requirement: R-WEB08 Keyboard
Outside text fields and without Ctrl, Alt or Cmd, `/` SHALL focus search, `n` open Add job, `[` fold the menu into icons, Esc close a window and `?` list the shortcuts. Going through new jobs one by one, Enter SHALL open the job, A mark it applied, → leave it for later and X mark it not a fit.

#### Scenario: Typing in a field
- **WHEN** the user types "n" in the search box
- **THEN** the letter goes into the search box and Add job does not open

### Requirement: R-WEB09 Great-match notifications
Desktop notifications for great matches SHALL be opt-in per browser. While the board is open, a job newly rated A or B and not yet applied to SHALL show a notification that opens its job; at most 3 notify at once, followed by one "N more" notification. Jobs already rated when notifications are switched on MUST NOT notify.

#### Scenario: Five new matches
- **WHEN** five open jobs are newly rated A or B while notifications are on
- **THEN** three notifications show, then one saying "2 more great matches"

### Requirement: R-WEB10 Other pages
The server SHALL serve a landing page at `/welcome`, a 404 page for unknown paths, a JSON 404 for unknown `/api/` paths, a redirect from `/app/` to `/`, and `GET /healthz` answering `{"status":"ok"}` without a token.

#### Scenario: Unknown page
- **WHEN** a browser opens `/nope`
- **THEN** it gets the 404 page with status 404
