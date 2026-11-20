# job-discovery Specification

## Purpose
Finding new jobs without the user pasting them: braveduck's own crawlers for VietnamWorks and CareerLink, the keyword lists that steer and filter them, per-source schedules, run reports from external tools, and followed Facebook groups whose public posts a scanner hands over.

## Requirements

### Requirement: R-CRW01 Built-in crawlers use the user's search terms
braveduck SHALL ship two built-in crawlers, `vietnamworks` and `careerlink`, which search for every term in the `search_queries` keyword list. With no search terms, a crawl started by hand MUST fail with a hint to add terms, and scheduled crawls SHALL be skipped without being reported as failing.

#### Scenario: No search terms yet
- **WHEN** the user runs `braveduck crawl vietnamworks` with an empty `search_queries` list
- **THEN** the command fails and says how to add search terms

### Requirement: R-CRW02 Keyword filters
Crawled listings SHALL pass four filter lists, in order: `positive_title` (when not empty, the title must contain one of its terms), `negative_title`, `negative_content` (title or description) and `negative_company`, each rejecting on any match. Matching SHALL ignore case and Vietnamese accents. Every list MUST start empty on a new board.

#### Scenario: Rejected by company
- **WHEN** `negative_company` holds "thẩm mỹ" and a listing's company is "Tham My Viet"
- **THEN** the listing is counted as filtered and not added

### Requirement: R-CRW03 Keyword list limits
Saving keyword lists MUST refuse an unknown list name, more than 200 terms in one list, or a term over 100 characters. Terms SHALL be trimmed and de-duplicated, and lists not named in a save SHALL be left unchanged.

#### Scenario: Too many terms
- **WHEN** a save sends 201 search queries
- **THEN** it fails and no list changes

### Requirement: R-CRW04 Per-source schedules
Each source SHALL have one schedule setting in minutes, where 0 means off. Defaults: 120 minutes for each built-in crawler, 360 for the TopCV status sync (`topcv-sync`) and 30 for automatic rating (`jev-rate`). A non-zero interval MUST be between the minimum and 1440 minutes; the minimum is 15 for built-in sources on the user's own computer, 60 on a shared server, and 5 for external tools that read their own setting.

#### Scenario: Too frequent on a shared server
- **WHEN** a user on a shared server sets `careerlink` to every 30 minutes
- **THEN** the change is refused with the allowed range 60 to 1440

#### Scenario: Turn a source off
- **WHEN** the user runs `braveduck crawl-schedule careerlink off`
- **THEN** CareerLink no longer crawls on its own

### Requirement: R-CRW05 Background work runs only while the board runs
Scheduled crawls, the TopCV sync, automatic rating, the expiry sweep, distance and company lookups and the daily backup SHALL run only inside `braveduck serve` (or each user's board on a shared server), checked once a minute. A due source SHALL be skipped when it is turned off on the board, and the TopCV sync SHALL be skipped while there is no live TopCV login, without either being reported as failing.

#### Scenario: Board stopped
- **WHEN** `braveduck serve` is not running
- **THEN** no crawl, sync, rating or sweep happens until it starts again or the user runs one by hand

### Requirement: R-CRW06 Polite crawling
Only one crawl of a given job site SHALL run at a time in the whole process (on a shared server, across all users), a second crawl of a source that is already running MUST be refused, and one crawl MUST stop after 15 minutes.

#### Scenario: Two users, one site
- **WHEN** two users' scheduled VietnamWorks crawls fall due at the same time on a shared server
- **THEN** one waits until the other's crawl ends

### Requirement: R-CRW07 Crawl outcome and reporting
A crawl SHALL report how many listings were found, filtered, already on the board, and added. A run where some queries fail but listings were found SHALL count as a successful run; a run where every query fails and nothing was found SHALL count as failed. Every built-in run, success or failure, SHALL be recorded for the crawl status panel.

#### Scenario: One query fails
- **WHEN** one of three search terms errors and the others return listings
- **THEN** the run is recorded as successful with the listings that were found

### Requirement: R-CRW08 External tools report their runs
An external tool SHALL be able to record a successful or failed run for its source (`braveduck crawl-report`). The source id MUST be 1–32 lowercase letters, digits, `-` or `_`, starting with a letter or digit; a failure reason SHALL be cut to 300 characters. A failure that follows a success SHALL be written to the activity feed once, not on every repeat.

#### Scenario: Repeated failures
- **WHEN** fb-watcher reports three failed runs in a row after a success
- **THEN** the activity feed gets one failure line

### Requirement: R-CRW09 Crawl status panel
Crawl status SHALL list each source that has run with its last run, last error, jobs saved today and health, plus how many jobs were saved today in total. A source SHALL count as healthy when its last run succeeded within one hour or within twice its interval, whichever is longer. A source the user turned off on the board SHALL show as healthy, since its silence is expected.

#### Scenario: Source off
- **WHEN** CareerLink is turned off and hasn't run for three days
- **THEN** crawl status does not flag CareerLink as unhealthy

### Requirement: R-CRW10 Following Facebook groups
A user SHALL be able to follow a Facebook group by its link, list followed groups with their name and visibility (checking, public, private or unavailable), pause or resume one, and unfollow one; unfollowing MUST leave its jobs on the board. A board MUST follow at most 30 groups.

#### Scenario: Thirty-first group
- **WHEN** a user who follows 30 groups pastes another group link
- **THEN** it is refused with a hint to unfollow one first

### Requirement: R-CRW11 Only public groups' matching posts come in
Posts handed over for a group SHALL become jobs only when the group is followed, recorded as public, not paused, and the Facebook source is on. Each post MUST have its own Facebook link and text, MUST contain at least one of the user's search terms (ignoring case and accents) and MUST pass the keyword filters; duplicates are skipped. At most 200 posts are taken per hand-over, and nothing is taken while the user has no search terms. A private group's posts SHALL be taken by nobody.

#### Scenario: Private group
- **WHEN** the scanner offers posts for a group recorded as private
- **THEN** the hand-over is refused and no job is added

#### Scenario: Post without a matching term
- **WHEN** a public group's post contains none of the user's search terms
- **THEN** it is skipped

### Requirement: R-CRW12 Group visibility notices
When the scanner reports a followed group as private for the first time, the board SHALL say in the activity feed that only members can see its posts, so the user should paste the ones they want. A group name SHALL be cut to 200 characters and a note to 300.

#### Scenario: Group turns private
- **WHEN** the scanner reports a followed group as private
- **THEN** the activity feed tells the user to paste the posts they want from it
