# Spec: platform sessions (log in once, reuse safely)

Status: **built** (approved 2026-11-01) · Item 15 of the upgrade plan · **TopCV verified on the live site 2026-11-04**; VietnamWorks, JobOKO, CareerLink pending a first real login
Depends on: built-in browser (`internal/core/browser`), service layer, MCP.
Unblocks: auto-apply, status sync, crawling behind a login (each a later task).

## Goal

Let braveduck act on job sites **as the user** — check application status,
later apply — without ever seeing or storing a password. The user logs in
themselves in a real browser window; braveduck keeps the resulting session,
encrypted, and reuses it headlessly until it expires.

This task delivers **sessions only**: connect, check, disconnect, list. It does
not apply to jobs or sync statuses — those are separate tasks that consume a
session.

## Decisions (approved)

- Browser: **stealth (CloakBrowser), one stable fingerprint per site**.
- Encryption key: **key file**, or the OS keychain when the machine has one.
- Built-in sites: **TopCV, VietnamWorks, JobOKO, CareerLink**. Others can
  be added through `platforms.json`; LinkedIn isn't built in.

## Evidence from the user's API studies

[`topcv-api-analysis`](https://github.com/khangpworking/topcv-api-analysis) and
[`vietnamworks-api-analysis`](https://github.com/khangpworking/vietnamworks-api-analysis)
(both read-only, sanitized) settle several open questions:

- **TopCV logged-in check is verified, not guessed.** Authenticated pages render
  `app.auth.isAuth = true` and a `window.topcvUser` object. The probe loads an
  account page (`/viec-lam-da-luu`) and evaluates that flag in the page.
- **TopCV cookies** (`docs/security.md`): `topcv_session` (~90 days) is the
  login, `XSRF-TOKEN` is a *session* cookie, `sso_token` (~12 h) is a helper,
  and `cf_clearance` (~1 year) is fingerprint-bound. So the "log in again" hint
  follows `topcv_session` (`auth_cookies`), not the earliest cookie. A test
  covers restoring a session cookie like `XSRF-TOKEN`.
- **TopCV account label**: `window.topcvUser` carries the email, so the
  Logins panel shows it masked (`ng…@gmail.com`).
- **Cloudflare challenges are intermittent** and clear on retry: probes wait
  up to 20s for a "Just a moment…" page to clear, and never try to defeat it.
  A page still on the challenge reads as "unknown", not "logged out".
- **TopCV sessions are fingerprint-bound.** Cloudflare ties trust to the TLS
  fingerprint (plain clients get `403`), so a session is only replayed from the
  same browser binary with the same fingerprint seed. That confirms the stable
  per-site fingerprint and rules out a plain HTTP client for TopCV.
- **VietnamWorks** documents no login or account *page*, only the login API on
  `secure.vietnamworks.com` (`docs/endpoints.md`), so its login and probe URLs
  are unverified guesses. Note: `/job-search/all-jobs` echoes the request's
  `Cookie` header into the HTML (`docs/security.md` §1). braveduck never logs
  page HTML, and the session probe must not use that route.
- **VietnamWorks** candidate APIs (`ms.vietnamworks.com/api-gateway/...`)
  answer `HTTP 400 {"code":"US01"}` when there is no session, which is a clean
  logged-out signal. Auth is a Bearer `access_token` + `refresh_token` from
  `secure.vietnamworks.com`. **Where the web app keeps that token
  (cookie vs `localStorage`) was not observed**, because the study never logged
  in. Until the first real login confirms it, the VNW probe falls back to the
  generic check (an account page doesn't bounce to the login page), and the
  session captures both cookies and `localStorage`.
- JobOKO and CareerLink have no study yet, so they use the generic check.

## What changed since ARCHITECTURE.md §1.2–1.4

| Original plan | Finding | Proposal |
|---|---|---|
| Mode A: visible login window | Works, incl. from WSL (WSLg) | **Keep** — the only mode in v1 |
| Mode B: attach to the user's everyday Chrome over CDP | Chrome 136+ refuses remote debugging on the default profile (anti-cookie-theft change), so "attach to the Chrome you're already logged into" no longer works | **Drop** from v1 |
| Encrypt with the OS keychain | No Secret Service on WSL/headless Linux (verified on the dev box) | Keychain when present, else a key file (see Security) |
| Platforms hard-coded to TopCV/VNW/JobOKO/CareerLink | — | **Platform registry**: built-in entries for those four, plus user-added sites, so it works for any country |

## User experience

```text
$ braveduck login topcv
Opening a browser window — log in to TopCV there (password, OTP, captcha:
whatever the site asks). Close the window when you're done.
✓ Logged in to TopCV as nguyen…@gmail.com — session saved (encrypted).

$ braveduck platforms
PLATFORM      STATUS        SINCE        CHECKED
topcv         connected     2026-11-02   2 min ago
vietnamworks  not connected
linkedin      expired       2026-10-01   yesterday   → braveduck login linkedin

$ braveduck logout topcv
✓ TopCV session deleted.
```

Board: a "Platforms" panel with each site's status and Connect/Disconnect
buttons. Connect opens the login window on the user's desktop. Agents
(MCP): `list_platforms`, `connect_platform`, `disconnect_platform`. These are
currently stubs.

## Design

### 1. Platform registry

A platform is data, not code:

```go
type Platform struct {
    ID         string // "topcv"
    Name       string // "TopCV"
    LoginURL   string // where the login window opens
    Domains    []string // cookies kept for these (e.g. ".topcv.vn")
    // Logged-in probe: open ProbeURL with the session; logged in when the
    // final URL doesn't match LoginPattern and (optionally) Selector exists.
    ProbeURL     string
    LoginPattern string // regexp on the final URL that means "bounced to login"
    // Optional JS evaluated in the probe page: true = logged in, false = not,
    // anything else = unknown. TopCV: `window.app?.auth?.isAuth === true`.
    ProbeJS      string
}
```

- Built-in: topcv, vietnamworks, joboko, careerlink.
- User-added: `~/.braveduck/platforms.json` with the same fields, so a
  user in Germany can add StepStone without a code change.
- **Except TopCV's (verified, see Evidence), the built-in probes are educated guesses until verified
  against real logged-in accounts.** I can't test them: I don't have, and must
  not use, anyone's credentials. The first `braveduck login <site>` by the user
  is the real test. If a probe is wrong it fails safe: the status shows
  "unknown", and nothing breaks.

### 2. Login flow (Mode A)

1. Launch the built-in Chromium, **headed**, with a fresh temporary profile.
   Use a **stable fingerprint per platform**: the seed is generated once and
   stored with the session. Today's random-per-launch fingerprint is right for
   anonymous fetching, but on a logged-in account it looks like a stolen
   session and invites a forced logout or ban.
2. Navigate to `LoginURL`. The user logs in by hand. braveduck reads no form
   fields and injects no scripts into the login page.
3. Finish when the user closes the window. Also poll the probe in the
   background, and show "✓ detected — you can close the window" once it
   passes. A 10-minute timeout cancels everything.
4. Capture cookies for `Domains` only (via CDP `Storage.getCookies`, filtered),
   plus `localStorage` for those origins. Then delete the temporary profile.
5. Run the probe headlessly with the captured session. Save it only if the
   probe passes or is unknown. Saving is refused when the probe says
   "bounced to login".

### 3. Using a session (for later tasks)

`browser.WithSession(ctx, platformID) (*rod.Browser, error)` launches headless
with the same fingerprint seed, restores the cookies and storage, and returns a
browser. Any consumer (apply, sync, crawl) goes through this one function. A
probe failure marks the session `expired`, and the board and agent then say
"run `braveduck login topcv`".

### 4. Storage

The existing `platform_sessions` table, extended by migration 3:

```sql
platform     TEXT PRIMARY KEY
session_enc  BLOB NOT NULL      -- AES-256-GCM(cookies + storage JSON)
created_at   TEXT NOT NULL
expires_hint TEXT               -- earliest auth-cookie expiry
fp_seed      TEXT NOT NULL      -- stable fingerprint for this account
status       TEXT NOT NULL      -- connected / expired / unknown
checked_at   TEXT
account_hint TEXT               -- masked label if the probe can read one, e.g. "nguyen…@gmail.com"
```

## Security

Threat model: protect the session at rest from casual copying (backups,
synced folders, someone grabbing the `.db`), and never expose it over the
network. It does **not** defend against malware running as the user. Nothing
on a normal desktop can; the browser's own cookie store has the same limit.

- **Never a password.** braveduck doesn't read login forms. The user types into
  the site itself.
- **Encryption at rest:** AES-256-GCM, a random nonce per write, and the
  platform ID as associated data, so blobs can't be swapped between platforms.
- **Key**, in order of preference:
  1. OS keychain (macOS Keychain, Windows Credential Manager, Secret Service)
     via `zalando/go-keyring`, which needs no cgo. This is the cross-platform
     library the architecture doc named, minus its cgo dependency.
  2. Otherwise a 32-byte key file `~/.braveduck/session.key`, mode `0600`, in a
     `0700` directory. On WSL that's the realistic option. `doctor` states which
     one is in use.
  3. Optional: `BRAVEDUCK_PASSPHRASE`. A key derived with Argon2id instead of
     the key file, for users who want sessions unreadable without a passphrase.
     The cost is re-entering it every time the session is used.
- **Never over the network:** no REST endpoint, MCP tool or log line ever
  returns cookie values. `list_platforms` returns status only.
- **Logout really deletes:** the row is removed and the temporary profile dirs
  are gone. A `braveduck logout --all` is included.
- **Board-triggered login** opens a window on the user's own desktop, so a
  web page can't use it to phish. It goes through the existing token- and
  Host-checked API.
- **`~/.braveduck` tightened to `0700`.** It currently defaults to `0755`, and
  the DB is `0644`.

## Risks the user should accept

- **Terms of service.** Automated access to job sites, apply especially, may
  breach a site's ToS and can get an account limited or banned. Sessions alone
  are low risk: the user logs in, and braveduck reads their own pages. The risk
  grows with the later auto-apply task, which will be opt-in per platform,
  rate-limited and use the existing cooldown logic.
- **Stealth browser.** The built-in CloakBrowser spoofs its fingerprint. That's
  defensible for the user acting on their own account, but it is a choice. The
  alternative is plain Chromium, which is more honest and more likely to get
  captchas or blocks.
- **Unverifiable probes:** see §1.

## Environment requirement

Any Chromium (the built-in CloakBrowser included) needs `libnss3`, `libnspr4`
and `libasound2`. Minimal WSL/Ubuntu images lack them:
`sudo apt install -y libnss3 libasound2t64`. `doctor` checks for them and
prints that command. This already affects today's "Extract & Add" browser
fallback.

## Out of scope

Apply, status sync, crawling behind a login, Mode B attach, multi-account per
platform, and sharing sessions between machines.

## Done when

- `braveduck login|logout|platforms`, board panel, and MCP
  `list_platforms`/`connect_platform`/`disconnect_platform` work end to end
  against a **local fake job site**: a test server with a login form and a
  members-only page.
- Tests: encryption round-trip, tamper and wrong-platform rejection, key-source
  selection, probe states, migration 3, no cookie values in any API, MCP or log
  output, logout deletes.
- `doctor` reports the key source and each session's status.
- The user does one real `braveduck login topcv` and confirms the status is
  correct; probe fixes from that run go in the same PR.
