# Spec Delta

## RENAMED Requirements

- FROM: `### Requirement: R-RAT04 Jev is the default rater, using the full profile`
- TO: `### Requirement: R-RAT04 Clef is the default rater, using the full profile`

- FROM: `### Requirement: R-RAT05 Automatic Jev rating on by default, with a per-user opt-out`
- TO: `### Requirement: R-RAT05 Automatic rating on by default, with a per-user opt-out`

## MODIFIED Requirements

### Requirement: R-RAT04 Clef is the default rater, using the full profile
Owner choice (2026-11-20, replacing the choice of 2026-11-04): Clef (`clef-flash`) SHALL be the rating model of a user who has not picked one. A board whose saved raters already include Jev SHALL keep Jev until the user switches. braveduck SHALL send the decision model the user's full profile (template scaffolding removed) with each job's title, company, location, salary, skills and description, rather than a condensed profile. `braveduck jev status` SHALL say what is sent.

#### Scenario: No rating model picked
- **WHEN** a user who never picked a rating model and has no saved raters opens Settings → Rating
- **THEN** the rating model shown is Clef with `clef-flash`

#### Scenario: Board already rating with Jev
- **WHEN** a board whose saved raters include Jev is upgraded and its user never picked a rating model
- **THEN** Jev stays the rating model and the badge's rater is unchanged

#### Scenario: Jev status
- **WHEN** the user runs `braveduck jev status`
- **THEN** it names the model and where its key comes from (never the key)
- **AND** it says the full profile and each job's text are sent to Cloudflare's API (or to TypeSafe's, when Jev is the rating model)

### Requirement: R-RAT05 Automatic rating on by default, with a per-user opt-out
Owner choice (2026-11-06): automatic decision-model rating SHALL be on by default, with a notice and a per-user opt-out. When a decision-model key is available and the user has never picked a rater, the scheduled run or the board's Rate button SHALL make a decision model the default rater by itself: Clef when a Clef token is saved, Jev when a Jev key is the only key available. A rater the user picked MUST be kept. The board's rating window SHALL say in one line that the profile and jobs are sent to the decision model to rate them.

#### Scenario: Clef token present, no rater picked
- **WHEN** a Clef token and account ID are saved and the board has no saved rater configuration
- **THEN** the next scheduled run makes `clef-flash` the default rater and rates new jobs

#### Scenario: Both keys present, no rater picked
- **WHEN** a Clef token and a Jev key are both available and the board has no saved rater configuration
- **THEN** the next scheduled run makes `clef-flash` the default rater, not Jev

#### Scenario: Key present, no rater picked
- **WHEN** a Jev key is available, no Clef token is saved and the board has no saved rater configuration
- **THEN** the next scheduled run makes Jev the default rater and rates new jobs

#### Scenario: User picked another rater
- **WHEN** the user saved a configuration whose default is their own AI
- **THEN** no decision model takes the badge on its own
