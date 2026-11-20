# job-pipeline Specification

## Purpose
The life of a job on the board after it arrives: the lanes it moves through, the edits and flags a user can set, the clocks that close stale jobs, and the shared views (Today, the interview rate, deal-breakers, search) that the board, the CLI and the MCP server all compute the same way.

## Requirements

### Requirement: R-PIP01 Six lanes in a fixed order
A job SHALL be in exactly one of six lanes, in this order: saved, waiting, applied, interview, offer, closed. A move to any other stage name MUST be refused, and stage names SHALL be matched without regard to case or surrounding spaces.

#### Scenario: Unknown stage
- **WHEN** the user runs `braveduck status 12 rejected`
- **THEN** the command fails and lists the six valid stages

### Requirement: R-PIP02 Edit limits
Edited text fields SHALL be trimmed; a title MUST NOT be blank; and a field MUST be refused when it is longer than its limit: title 200, company 200, salary 100, location 300, notes 20000 characters. Only the fields named in an edit change.

#### Scenario: Blank title
- **WHEN** an edit sets the title to "   "
- **THEN** the edit fails with "title can't be blank" and the job is unchanged

### Requirement: R-PIP03 Applying starts the no-answer clock
Moving a job from Saved or Waiting to Applied SHALL record today as its applied date, unless the job already has an applied date. That date drives Today's follow-ups and the no-answer sweep.

#### Scenario: Move to Applied
- **WHEN** a Saved job with no applied date is moved to Applied on 2026-11-20
- **THEN** its applied date is 2026-11-20

### Requirement: R-PIP04 Expiry sweep moves stale jobs to Closed
While the board runs, an hourly sweep SHALL move to Closed: a Saved or Waiting job whose posting deadline passed 7 or more days ago, and an Applied job with no answer 27 days (20 days of silence plus 7 days of countdown) after its applied date. Each moved job SHALL get a note that starts "Closed automatically:" and says why. Nothing is deleted. Pinned and kept jobs, and jobs in Interview or Offer, MUST NOT be moved. A job edited or moved since the sweep read it SHALL be left alone.

#### Scenario: Deadline long past
- **WHEN** a Saved job's deadline was 8 days ago and it is neither pinned nor kept
- **THEN** the next sweep moves it to Closed with a note naming the closing date

#### Scenario: Pinned job
- **WHEN** a pinned Applied job has had no answer for 40 days
- **THEN** the sweep leaves it in Applied

### Requirement: R-PIP05 Undoing the sweep keeps the job
When a job the sweep closed is moved out of Closed, it SHALL be marked kept, so the next sweep does not close it again.

#### Scenario: Drag back
- **WHEN** the user moves a swept job from Closed back to Saved
- **THEN** the job is in Saved with keep turned on

### Requirement: R-PIP06 Expiry clock shown on each job
Each Saved or Waiting job with a deadline SHALL show a deadline clock (ok, warning at 10 days or fewer, expired once past, then the days until the sweep closes it), and each Applied job with an applied date SHALL show a silence clock (silent for under 20 days, then counting down to the sweep). Other jobs have no clock.

#### Scenario: Close deadline
- **WHEN** a Saved job's deadline is 6 days away
- **THEN** its clock state is warning with 6 days left

### Requirement: R-PIP07 Today
Today SHALL list, in this order and leaving out empty groups: best matches (Saved or Waiting jobs rated A or B, best score first), deadlines this week (Saved or Waiting jobs whose deadline is 0 to 7 days away, soonest first), and follow-ups (Applied jobs at least 7 days after their applied date, or after the last follow-up when one was recorded). Each group MUST show at most 3 jobs. The board, `braveduck today` and the MCP `get_today` SHALL return the same groups.

#### Scenario: Nothing to act on
- **WHEN** no job qualifies for any group
- **THEN** Today is empty

#### Scenario: Many great matches
- **WHEN** five open jobs are rated A or B
- **THEN** best matches lists the three highest scores

### Requirement: R-PIP08 Recording a follow-up
The user MAY record that they asked a company about an application; this MUST only be allowed for a job in Applied, braveduck SHALL send nothing itself, and Today SHALL stop listing that job until another 7 days pass without an answer.

#### Scenario: Follow up a Saved job
- **WHEN** the user marks a Saved job as followed up
- **THEN** it fails: only an application still waiting for an answer can be followed up

### Requirement: R-PIP09 Interview rate and morale line
The interview rate SHALL be interviews divided by resolved jobs, where Interview and Offer jobs are resolved wins, every Closed job is resolved, and an Applied job is resolved once 14 days have passed since its applied date (younger ones are pending). Below 3 resolved, the morale line MUST say it is too early; otherwise it SHALL pick one of three messages (rate under 10%, under 30%, 30% or more). Both come from the server, the same on every face.

#### Scenario: Too early
- **WHEN** two jobs are resolved
- **THEN** the morale line says it is too early to calculate a rate

### Requirement: R-PIP10 Deal-breakers computed once on the server
Each job returned by any face SHALL carry the same deal-breaker labels, shown in Vietnamese: Saturday work (from the title and job text, unless a five-day week or Saturday off is stated, while an explicit extra Saturday still counts), negotiable or undisclosed pay (from the salary field only), too far (company distance above the user's maximum commute, default 10 km, ignored beyond 80 km as a bad geocode), company younger than 2 years, and company no longer operating.

#### Scenario: Negotiable pay
- **WHEN** a job's salary reads "Thỏa thuận"
- **THEN** the job carries the label "Lương thỏa thuận"

#### Scenario: Wrong-province geocode
- **WHEN** a job's company geocodes 900 km away
- **THEN** no "too far" label is shown

### Requirement: R-PIP11 Search
A search SHALL keep a job only when every word of the query appears in its title, company, location, skills, salary, notes or source, ignoring case and Vietnamese accents (including đ).

#### Scenario: Accents optional
- **WHEN** the user searches "ky su da nang"
- **THEN** a job titled "Kỹ sư" located in "Đà Nẵng" is found

### Requirement: R-PIP12 Pin and keep
The user SHALL be able to toggle pin and keep on any job. Pinned jobs sort first in their list; kept and pinned jobs are exempt from the expiry sweep.

#### Scenario: Keep a job
- **WHEN** the user toggles keep on a Saved job whose deadline passed long ago
- **THEN** the sweep no longer closes it

### Requirement: R-PIP13 Deleting a job
Deleting a job MUST remove the job, its ratings and its tailored CV files permanently.

#### Scenario: Delete
- **WHEN** the user deletes job 12, which has two tailored CV versions
- **THEN** job 12, its ratings and both CV files are gone

### Requirement: R-PIP14 Activity feed
Adds, lane moves, removals, ratings, logins and background outcomes SHALL be written to an activity feed, newest first. Writing to the feed MUST never fail the change that caused it, and the feed SHALL keep at most the newest 5000 entries.

#### Scenario: Move logged
- **WHEN** a job is moved from Saved to Applied
- **THEN** the feed shows "Moved "<title>" to applied"
