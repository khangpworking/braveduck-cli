# rating Specification

## Purpose
Every job gets an A–F grade with reasons, judged against the user's own profile. A rater is either a decision model that rates on the board itself (Jev from TypeSafe, or Cloudflare's Clef) or the user's own AI, which gets a brief and submits a verdict. One rater owns the badge; up to two more may rate alongside it as challengers.

## Requirements

### Requirement: R-RAT01 Score scale and verdict rules
A score MUST be between 1.0 and 5.0 and its letter one of A, B, C, D, F, with A at 4.5 or more, B 4.0–4.4, C 3.5–3.9, D 3.0–3.4 and F below 3.0. When a verdict has no letter it SHALL be derived from the score. Reasons MUST be at least 10 characters. Every face (CLI, REST, MCP, decision models) SHALL apply the same checks.

#### Scenario: Out-of-range score
- **WHEN** an agent submits a score of 5.5
- **THEN** the verdict is refused with "score must be 1.0-5.0"

#### Scenario: Letter left out
- **WHEN** a verdict has score 4.2 and no letter
- **THEN** it is stored as B

### Requirement: R-RAT02 No rating without a profile
While the profile is still the blank template, braveduck MUST NOT build a rating brief or rate with a decision model; it SHALL tell the user which profile to fill in. The text a rater reads SHALL be the profile with the template's comments, empty labels and empty headings removed.

#### Scenario: Empty profile
- **WHEN** the user runs `braveduck rate` before filling in the profile
- **THEN** the command fails with "your profile is empty" and the profile's location

### Requirement: R-RAT03 Rater configuration
The rater configuration SHALL name 1 to 3 distinct, non-empty model ids, one of which is the default. The default rater's verdict SHALL own the board's badge; with A/B on, the other models rate as challengers whose verdicts are kept beside it; with A/B off only the default rates. A save that breaks these rules MUST be refused, while a stored value that breaks them SHALL be repaired on read. With nothing saved, the default rater is an AI-agent model id, not a decision model.

#### Scenario: Default not in the set
- **WHEN** a save names models "a" and "b" with default "c"
- **THEN** it is refused with "default model must be one of the selected models"

#### Scenario: Challenger verdict
- **WHEN** A/B is on and a challenger model submits a verdict
- **THEN** the verdict is stored for that model and the badge is unchanged

### Requirement: R-RAT04 Jev is the default rater, using the full profile
Owner choice (2026-11-04): Jev SHALL be the default rater, and braveduck SHALL send it the user's full profile (template scaffolding removed) with each job's title, company, location, salary, skills and description, rather than a condensed profile. `braveduck jev status` SHALL say what is sent.

#### Scenario: Jev status
- **WHEN** the user runs `braveduck jev status`
- **THEN** it names the model and where its key comes from (never the key)
- **AND** it says the full profile and each job's text are sent to TypeSafe's API (or to Cloudflare's, when Clef is the rating model)

### Requirement: R-RAT05 Automatic Jev rating on by default, with a per-user opt-out
Owner choice (2026-11-06): automatic Jev rating SHALL be on by default, with a notice and a per-user opt-out. When a decision-model key is available and the user has never picked a rater, the scheduled run or the board's Rate button SHALL make the decision model the default rater by itself; a rater the user picked MUST be kept. The board's rating window SHALL say in one line that the profile and jobs are sent to the decision model to rate them.

#### Scenario: Key present, no rater picked
- **WHEN** a Jev key is available and the board has no saved rater configuration
- **THEN** the next scheduled run makes Jev the default rater and rates new jobs

#### Scenario: User picked another rater
- **WHEN** the user saved a configuration whose default is their own AI
- **THEN** Jev does not take the badge on its own

### Requirement: R-RAT06 Opt-out covers both decision models
A user who ticks "Don't send my profile to Jev" (or to Clef) SHALL get no automatic rating and no decision-model rating on demand, from either Jev or Clef. Their Rate button SHALL then ask their own AI: a rater kept as a challenger, else the out-of-box agent rater, whose verdict owns the badge. The choice MUST be stored in that user's own database, so on a shared server one user's opt-out never affects another; unticking it turns the decision model back on.

#### Scenario: Rate after opting out
- **WHEN** a user who opted out presses Rate on a job
- **THEN** braveduck returns a brief for the user's own AI and sends nothing to Jev or Clef

### Requirement: R-RAT07 Scheduled decision-model rating
The `jev-rate` source SHALL, on its schedule (every 30 minutes by default), rate up to 20 unrated jobs that are not Closed, oldest first, and stop at the first API error. It SHALL do nothing when there is no key, another rater was picked, or the user opted out. An empty profile or a used-up daily limit MUST NOT be reported as a failure.

#### Scenario: API error
- **WHEN** the decision model fails on the third job of a run
- **THEN** the run stops there and is recorded as failed in crawl status

### Requirement: R-RAT08 How a decision model's answer becomes a grade
Jev and Clef SHALL be asked the same multiple-choice questions: fit (A–F), skills, role, seniority, pay and deal-breaker. The score SHALL be the sum of each fit letter's probability times its band midpoint (A 4.75, B 4.2, C 3.7, D 3.2, F 2.0), rounded to 0.1, and the letter SHALL follow from the score so the badge never disagrees with its number. The reasons SHALL start with the model's name, fit choice and confidence, then the sub-answers, leaving out unclear ones.

#### Scenario: Reasons line
- **WHEN** Jev answers fit C with 57% and skills partial
- **THEN** the reasons start "Jev: C (57% sure) • skills: partial match"

#### Scenario: Answer options
- **WHEN** a decision model is asked to rate a job
- **THEN** skills is one of strong, partial, weak or unclear; role on target, adjacent, off target or unclear; seniority match, above, below or unclear; pay meets, below or unclear; and deal-breaker none or some

### Requirement: R-RAT09 Clef as an alternative decision model
Owner choice (2026-11-20): Cloudflare's Clef MAY rate instead of Jev. The user SHALL pick the rating model (Jev or Clef) and, for Clef, `clef-flash` (default) or `clef`. Switching provider or Clef model MUST replace the decision model among the saved raters with the new one, keeping its role (badge or challenger); a board with no saved rater configuration SHALL be left alone. Picking Jev or Clef as default rater SHALL also switch the rating model.

#### Scenario: Switch to Clef
- **WHEN** Jev owns the badge and the user switches the rating model to Clef
- **THEN** `clef-flash` owns the badge and later reasons start with "Clef:"

### Requirement: R-RAT10 Decision-model keys are write-only
A Jev key and a Clef token pasted in Settings SHALL be sealed with the session vault, bound to their slot, before they reach the database, and MUST never be returned, logged or shown again: the board only learns whether one is saved and where the key in use comes from. A pasted Jev key SHALL win over the server's environment key or key file. A key with spaces or line breaks, or a Clef account ID that isn't 32 letters and digits, MUST be refused. An empty key removes the pasted one.

#### Scenario: Read settings
- **WHEN** the board asks for the decision settings after a key was pasted
- **THEN** the reply says a key is saved and contains no key

### Requirement: R-RAT11 Daily cap on the server's key
On a shared server, decision-model calls (ratings and lead checks) paid by the server's own key SHALL be capped per user per day (`--jev-daily-limit`, default 50; 0 means no cap). A user's own pasted Jev key or Clef token MUST NOT be capped. Past the cap, rating SHALL wait for the next day and say so.

#### Scenario: Cap reached
- **WHEN** a user's 51st Jev rating of the day would use the server's key
- **THEN** it fails with "today's Jev ratings are used up on this server"

### Requirement: R-RAT12 Bring your own AI
The user's own AI SHALL be able to fetch a rating brief (the profile, the rubric, the default rater id and the jobs) for the queue (unrated jobs that are not Closed, oldest first, 10 by default) or for one job, scored or not, and submit a verdict with its model id; job descriptions in a brief SHALL be cut to 6000 characters. An empty model id SHALL mean the default rater. Asking for a rating by a named challenger MUST NOT run the default decision model.

#### Scenario: Paste-ready brief
- **WHEN** the user runs `braveduck rate --limit 5`
- **THEN** a brief with up to five unrated jobs is printed, to paste into any AI chat

#### Scenario: Save a verdict from the CLI
- **WHEN** the user runs `braveduck rate 12 --score 4.2 --why "skills match, pay ok"`
- **THEN** job 12 gets B 4.2 from the default rater
