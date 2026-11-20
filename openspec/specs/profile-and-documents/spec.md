# profile-and-documents Specification

## Purpose
What the user tells braveduck about themselves and what they send to employers: the profile jobs are rated against, the uploaded base CV, the getting-started checklist, and tailored CVs and application emails that the user's own AI writes. braveduck stores and prints these; it never writes them itself and never sends email.

## Requirements

### Requirement: R-DOC01 The profile file
The profile SHALL be a Markdown file in the data dir, created from a template on first use with owner-only permissions, and an existing file MUST never be overwritten by that step. The profile counts as filled once any line survives after removing comments, headings, bare bullets and empty "Label:" lines. A saved profile MUST be refused when it is over 100,000 characters.

#### Scenario: First run
- **WHEN** the user runs `braveduck profile` on a new install
- **THEN** the profile file is created from the template and reported as not filled in

### Requirement: R-DOC02 Uploading the base CV
The user SHALL be able to upload one base CV as PDF, .docx, Markdown or HTML, up to 5 MB and 50,000 characters of text. The file SHALL be kept for applying, and its text SHALL go into the profile's "About me" section. When "About me" already has the user's own text, the upload MUST be refused unless the user asks to replace it.

#### Scenario: About me already written
- **WHEN** the user uploads a CV while "About me" has their own text and replace is not set
- **THEN** the upload fails asking whether to replace it, and the profile is unchanged

#### Scenario: File too big
- **WHEN** the user uploads a 7 MB PDF
- **THEN** the upload is refused as over 5 MB

### Requirement: R-DOC03 Getting started
The getting-started status SHALL report: base CV uploaded, "About me" filled, what the user is looking for filled (any other profile section), the number of search terms, and the optional location and TopCV login. It SHALL be done once "About me", what they want and at least one search term are in place.

#### Scenario: Profile done, no search terms
- **WHEN** "About me" and what the user wants are filled but there are no search terms
- **THEN** getting started is not done

### Requirement: R-DOC04 Tailored CVs from the user's own AI
For a job, braveduck SHALL give the user's AI a brief (profile, job, instructions, current version if any) and store the Markdown CV it returns as a new version, at most 100,000 characters and at most 30 versions per job. The current version SHALL be available as a PDF.

#### Scenario: Thirty-first version
- **WHEN** an agent submits a CV for a job that already has 30 versions
- **THEN** it is refused

#### Scenario: PDF
- **WHEN** the user runs `braveduck cv 12 --pdf out.pdf`
- **THEN** job 12's current CV is written as a PDF

### Requirement: R-DOC05 Application email drafts
braveduck MUST never send an email. A draft SHALL have an optional recipient (the job's contact email when none is given; when present it MUST be a valid address), a subject of 1–300 characters and a body of 20–20,000 characters, and is stored ready to review. When the user says they sent it from their own mail app, the draft SHALL be marked sent and a Saved or Waiting job moved to Applied; rejecting a draft discards it.

#### Scenario: Mark as sent
- **WHEN** the user runs `braveduck draft 12 --sent` for a Saved job with a draft
- **THEN** the draft is marked sent and job 12 moves to Applied

#### Scenario: Bad recipient
- **WHEN** a draft's recipient is "hr at example"
- **THEN** it is refused as not an email address

### Requirement: R-DOC06 Text is written by the user's AI
braveduck MUST NOT call an AI to write CVs or emails. Briefs for these SHALL be available both to an agent over MCP and as paste-ready text from the CLI (`braveduck cv <id>`, `braveduck draft <id>`), so any AI chat can do the writing.

#### Scenario: CLI brief
- **WHEN** the user runs `braveduck draft 12 --notes "mention my n8n work"`
- **THEN** a brief including those notes is printed for the user's AI
