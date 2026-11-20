# assisted-apply Specification

## Purpose
Helping the user apply on a job site where they are logged in, and keeping the board in step with what they applied to. braveduck prepares the CV and opens the job in a visible window with the user's login; the user submits. Afterwards braveduck confirms from TopCV's own history, or asks the user on other sites, and a read-only TopCV sync keeps application statuses current.

## Requirements

### Requirement: R-APL01 The user submits, never braveduck
braveduck MUST NOT click a submit button or fill an application form on a job site. Assisted apply SHALL open the job in a visible window with the site's saved login, and the user reviews and submits there themselves; the window SHALL stay open for up to 20 minutes.

#### Scenario: Apply on VietnamWorks
- **WHEN** the user presses "Apply on VietnamWorks" for a job
- **THEN** a visible window opens on the job page with their VietnamWorks login, and nothing is submitted by braveduck

### Requirement: R-APL02 When a job can be applied to
An assisted apply MUST be refused unless: the job's link is on a site braveduck can hold a login for, the user has a saved, not-expired login there, the job is not already applied to (lane Applied, Interview or Offer, or a TopCV status recorded), and there is a CV to send (the job's tailored CV printed to PDF, else the uploaded base CV). The apply SHALL hand the user the CV file and the draft email's body as a cover letter, when there is one.

#### Scenario: No CV yet
- **WHEN** the user starts an apply with no tailored CV and no uploaded CV
- **THEN** it fails asking them to upload their CV or tailor one first

#### Scenario: Not logged in
- **WHEN** the job is on JobOKO and the user has no JobOKO login
- **THEN** it fails with "log in to JobOKO first — braveduck login joboko"

### Requirement: R-APL03 Apply limits protect the account
Assisted applies MUST be limited to 10 in any 24 hours and at least 5 minutes apart. The attempt SHALL be recorded before the window opens, so applies started together can't all pass the limits.

#### Scenario: Too soon
- **WHEN** the user starts an apply 2 minutes after the previous one
- **THEN** it is refused, saying how many more minutes to wait

### Requirement: R-APL04 Confirming the application
After the window closes on TopCV, braveduck SHALL read the user's TopCV application history and move the job to Applied only when TopCV lists it. On any other site, the job SHALL ask "did you submit it?": yes moves it to Applied (starting the no-answer clock), not yet clears the question.

#### Scenario: TopCV doesn't list it
- **WHEN** the window closes and TopCV's history does not list the job
- **THEN** nothing changes and the user is told to try again when ready

#### Scenario: Confirm on CareerLink
- **WHEN** the user answers yes for a CareerLink apply
- **THEN** the job moves to Applied with the detail "applied on CareerLink (you confirmed)"

### Requirement: R-APL05 TopCV status sync
With a TopCV login, braveduck SHALL read the user's TopCV application history read-only, on demand (`braveduck sync topcv`) and every 6 hours while the board runs. For each application it SHALL record TopCV's status on the matching job, move a Saved or Waiting job to Applied (dated from TopCV's applied date when known), and add an application not yet on the board straight to Applied unless the TopCV source is off. A history page that redirects to the login page SHALL mark the session expired.

#### Scenario: Applied on the site directly
- **WHEN** the sync finds a TopCV application that isn't on the board
- **THEN** it is added in Applied with its TopCV status

#### Scenario: Login expired
- **WHEN** the sync's history page redirects to TopCV's login page
- **THEN** the sync fails, the session is marked expired, and the user is told to run `braveduck login topcv`

#### Scenario: Pasted copy of an application
- **WHEN** the sync finds an application whose job is on the board as pasted text with no TopCV link
- **THEN** that job is updated instead of a second card being added

### Requirement: R-APL06 Only on the user's own computer
Assisted apply MUST be refused on a shared server for everyone except the owner of the computer it runs on, since its window would open where nobody can see it.

#### Scenario: Shared-server user
- **WHEN** a user on a shared server who is not its owner starts an apply
- **THEN** it fails saying this opens a window on your own computer and is off on a shared server
