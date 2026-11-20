# platform-sessions Specification

## Purpose
Logging in to job sites so braveduck can act there as the user (read their application history, open an apply window) without ever seeing or storing a password. The user logs in themselves in a real browser window; braveduck keeps the resulting session, encrypted, and reuses it until it expires.

## Requirements

### Requirement: R-SES01 Never a password
braveduck MUST NOT read, fill or store a job-site password. A login SHALL open a visible browser window on the site's own login page, the user SHALL log in there themselves (password, one-time code, captcha), and braveduck MUST NOT read the login form or inject scripts into the login page.

#### Scenario: Log in to TopCV
- **WHEN** the user runs `braveduck login topcv`
- **THEN** a browser window opens on TopCV's login page and braveduck waits for the user to finish there

### Requirement: R-SES02 Approved session design
Decision approved by the owner (2026-11-01): the browser SHALL be the stealth browser with one stable fingerprint per site; the encryption key SHALL come from a key file, or the OS keychain when the machine has one; the built-in sites SHALL be TopCV, VietnamWorks, JobOKO and CareerLink, others MAY be added by the user in `platforms.json` in the data dir, and LinkedIn is not built in.

#### Scenario: Add a site
- **WHEN** the user adds a site entry with its login page, domains and a logged-in check to `platforms.json`
- **THEN** that site appears in `braveduck platforms` and can be logged in to

### Requirement: R-SES03 Login flow and when a session is saved
A login SHALL finish when the user closes the window and MUST be cancelled after 10 minutes. Only the site's own cookies and local storage SHALL be kept. A headless check then runs: the session SHALL be saved when the check passes or can't tell, and MUST NOT be saved when the site still shows the user as logged out. One login window per site SHALL be open at a time, and a new login SHALL keep the site's fingerprint so the account keeps seeing one device.

#### Scenario: Window closed without logging in
- **WHEN** the user closes the login window and the check bounces to the login page
- **THEN** nothing is saved and the user is told to try again

#### Scenario: Second login window
- **WHEN** a TopCV login window is already open and the user starts another
- **THEN** the second one is refused

### Requirement: R-SES04 Encryption at rest
Sessions SHALL be encrypted with AES-256-GCM, a fresh nonce per write and the platform id bound to each blob, so a blob can't be moved to another site. The key SHALL come from, in order: `BRAVEDUCK_PASSPHRASE` (Argon2id, per-install salt), an existing key file in the data dir (kept even if a keychain appears later), the OS keychain, else a new owner-only key file. On a shared server each user's sessions SHALL use that user's own key file only.

#### Scenario: No keychain on WSL
- **WHEN** braveduck first saves a session on a machine with no keychain and no passphrase set
- **THEN** it creates a key file readable only by the user and uses it

#### Scenario: Tampered blob
- **WHEN** a stored session can't be opened with the key
- **THEN** that session is marked expired and the user is asked to log in again

### Requirement: R-SES05 Session data never leaves the vault
No REST reply, MCP answer, CLI output or log line MUST ever contain cookie values or other session data. Listing sites SHALL return status only: not connected, connecting, connected, expired or unknown, with when it was saved and checked, an expiry hint, and a masked account label when the site shows one.

#### Scenario: List logins
- **WHEN** an agent calls `list_platforms`
- **THEN** each site shows its status and a masked account such as "ng…@gmail.com", and no cookie

### Requirement: R-SES06 Checking a session
Re-checking a saved login SHALL load a page of the site with the session and record the result. A page still behind a Cloudflare challenge after waiting up to 20 seconds SHALL read as unknown, not logged out; braveduck MUST NOT try to defeat the challenge. A session that fails the check SHALL be marked expired, and the board and agents then tell the user to log in again.

#### Scenario: Expired login
- **WHEN** `braveduck platforms check topcv` finds the account page bouncing to the login page
- **THEN** the TopCV session is marked expired

### Requirement: R-SES07 Logout really deletes
Logging out of a site SHALL delete its saved session; `braveduck logout --all` SHALL delete every saved session.

#### Scenario: Log out
- **WHEN** the user runs `braveduck logout topcv`
- **THEN** the TopCV session is gone and TopCV shows as not connected

### Requirement: R-SES08 Logins started from the board or an agent
A login started from the board or over MCP SHALL open the window in the background and answer at once with status connecting; the outcome SHALL be written to the activity feed. On a shared server a login MUST be refused for everyone except the owner of the computer it runs on (R-HST07).

#### Scenario: Agent starts a login
- **WHEN** an agent calls `connect_platform` for TopCV
- **THEN** the call returns status connecting and a later `list_platforms` shows the result
