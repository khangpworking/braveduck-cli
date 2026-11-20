# job-intake Specification

## Purpose
How a job gets onto the board: a pasted link or job text, a job described field by field, or a tool (crawler, sync, scanner) feeding the board. Intake reads what it can without any AI, refuses what it can't use, and never lets the same listing in twice.

## Requirements

### Requirement: R-ADD01 Supported links only
A pasted link MUST come from TopCV, VietnamWorks, JobOKO, CareerLink or Facebook (the site or a subdomain); a link from any other site SHALL be refused with a message naming the supported sites and asking for the job's text instead. Content that does not start with an http(s) link SHALL be read as pasted job text, with source `paste`.

#### Scenario: Link from an unsupported site
- **WHEN** the user pastes a link from a site that is not in the supported list
- **THEN** nothing is added
- **AND** the error names TopCV, VietnamWorks, JobOKO, CareerLink and Facebook and suggests pasting the job's text

#### Scenario: Plain job text
- **WHEN** the user pastes a job description with no leading link
- **THEN** a job is added from the text with discovery source `paste`

### Requirement: R-ADD02 Minimum content
Content shorter than 5 characters (after trimming) MUST be refused as invalid input.

#### Scenario: Too short
- **WHEN** the user submits "abc"
- **THEN** the board answers "paste a job link or description" and adds nothing

### Requirement: R-ADD03 No AI when reading a job
Intake SHALL read jobs without calling any AI: a job-site link is fetched and its schema.org JobPosting is copied as-is, pasted text is read with line and pattern rules, and a field that can't be read stays empty. A blocked or script-only page SHALL fall back to the pasted text, or to a card with just the link; a job with no readable title is titled "Untitled listing".

#### Scenario: Page can't be read
- **WHEN** the user pastes a TopCV link whose page returns no JobPosting and no text follows the link
- **THEN** a card is still added with the link and the title "Untitled listing"

### Requirement: R-ADD04 braveduck never opens Facebook
braveduck MUST NOT fetch a Facebook link itself. A Facebook post's text SHALL come from the text pasted after the link; when no text is pasted and the fb-watcher reader is connected, fb-watcher reads the post (public groups only); otherwise a bare "Facebook post" card is added. When fb-watcher can't read the post (private group, deleted, busy, unreachable), the add MUST fail with a message asking the user to paste the post's text.

#### Scenario: Post in a private group via fb-watcher
- **WHEN** fb-watcher is connected and the user pastes only the link of a post in a private group
- **THEN** nothing is added
- **AND** the message says the group is private and asks for the post's text under the link

#### Scenario: Link plus text
- **WHEN** the user pastes a Facebook post link followed by the post's text
- **THEN** the job is built from the pasted text and the link is stored in canonical form

### Requirement: R-ADD05 A group link follows the group
A link to a Facebook group itself (not one of its posts), given to the board's Add, the CLI `add` or the MCP `add_job`, SHALL follow that group instead of adding a job.

#### Scenario: Paste a group link
- **WHEN** the user pastes `https://www.facebook.com/groups/<key>/`
- **THEN** the group is added to the followed groups and no job is created

### Requirement: R-ADD06 Public addresses only
braveduck MUST only fetch public http(s) addresses; a link that resolves to localhost, a private network, carrier-grade NAT or another non-public address SHALL be refused outright, and the check SHALL hold again at connect time so a redirect or a DNS change can't reach such an address.

#### Scenario: Link to the local network
- **WHEN** a pasted link on a supported domain resolves to a private address
- **THEN** the add fails as invalid input and no request is made to that address

### Requirement: R-ADD07 No duplicate listings
A job already on the board MUST NOT be added again, however it arrives (paste, CLI, MCP, crawl, TopCV sync, Facebook hand-over). Two jobs SHALL be the same listing when they share a canonical link (no fragment or tracking parameters), the same job text of at least 50 characters (ignoring a short metadata footer), or the same title and company when one has no link and the existing one is not Closed. Two different links are two listings. The refusal SHALL name up to three matches.

#### Scenario: Same link with tracking parameters
- **WHEN** the user pastes a job link that is already on the board with an extra `?utm_source=` query
- **THEN** nothing is added and the error names the existing job

#### Scenario: Same title, two links
- **WHEN** a crawl finds a job with the same title and company as one on the board but a different link
- **THEN** it is added as a separate listing

#### Scenario: Closed job does not block a pasted copy
- **WHEN** the user pastes text (no link) whose title and company match a job that is in Closed
- **THEN** the new job is added

### Requirement: R-ADD08 Defaults for a new job
Every new job SHALL start in the Saved lane with its rating pending. The contact email and the apply method (email or on the site) SHALL be read from the job text, a deadline read from the posting SHALL start the job's deadline clock, and a job with a link but no source SHALL take the source of its link.

#### Scenario: JD with an email
- **WHEN** a pasted JD says "send your CV to hr@example.com"
- **THEN** the new job is in Saved, rating pending, with that contact email

### Requirement: R-ADD09 Adding field by field
A job described field by field (CLI `add --title`, MCP `add_job` with `title`) MUST have a non-blank title. When it names a source that is turned off on this board, it SHALL be refused.

#### Scenario: Missing title
- **WHEN** an agent calls `add_job` with a company but no title or content
- **THEN** the call fails with "title is required"

### Requirement: R-ADD10 Turning a job source off
A job source turned off SHALL stop its jobs coming onto the board from crawls, tools and the Facebook scanner. Nothing else changes: a saved login to that site stays, jobs already on the board stay, and a link the user pastes from that site is still added. A setting that can't be read SHALL turn nothing off.

#### Scenario: Source off, link pasted
- **WHEN** VietnamWorks is turned off and the user pastes a VietnamWorks link
- **THEN** the job is added

#### Scenario: Source off, crawl
- **WHEN** VietnamWorks is turned off
- **THEN** its scheduled crawl does not run and is not reported as failing

### Requirement: R-ADD11 Board size cap
When the board has a job cap (a shared server sets one, default 5000 per user; a board on the user's own computer has none), adding past the cap MUST be refused with a message that the board is full and old jobs should be deleted first.

#### Scenario: Full board
- **WHEN** a user's board on a shared server holds 5000 jobs and a new one is added
- **THEN** the add fails with "the board is full (5000 jobs)"

### Requirement: R-ADD12 Lead check for community sources
For sources on the lead-check list (default: `facebook`), a new post from a tool, crawl or scanner SHALL be classified by the chosen decision model; one that is "not a job offer" with at least 75% confidence SHALL be added straight to Closed with a note giving the reason, never dropped. Any error (no key, outage, daily limit) MUST keep the post in Saved. A job the user pastes skips the check. The user MAY turn the check on or off per source.

#### Scenario: A course advert from a group
- **WHEN** the scanner hands over a group post that the decision model calls not a job with 90% confidence
- **THEN** the job is stored in Closed with the note "Jev: not a job offer (90% sure) — closed automatically; drag it back if Jev got it wrong."

#### Scenario: Decision model unavailable
- **WHEN** no decision-model key is available and a Facebook post arrives from the scanner
- **THEN** the post is added to Saved

### Requirement: R-ADD13 Re-checking posts already on the board
The user SHALL be able to re-run the lead check over Saved jobs from checked sources (`braveduck jev check-leads`), closing the ones the decision model is sure are not jobs with a note, and MAY preview the result with `--dry-run`, which MUST change nothing.

#### Scenario: Dry run
- **WHEN** the user runs `braveduck jev check-leads --dry-run`
- **THEN** each Saved Facebook post is listed with the model's verdict and no job moves
