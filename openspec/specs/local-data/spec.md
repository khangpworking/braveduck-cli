# local-data Specification

## Purpose
Where braveduck keeps the user's job hunt on their own disk and the commands that look after it: the database location, file permissions, schema upgrades, backups, export and the `doctor` health check. Everything here runs on the user's machine with no account.

## Requirements

### Requirement: R-DAT01 Database location
The database SHALL be the file named by `--db`, else by `BRAVEDUCK_DB`, else `~/.braveduck/braveduck.db`. Its folder is the data dir, which holds the profile, the uploaded CV, tailored CVs, the session key, `platforms.json` and backups.

#### Scenario: Override
- **WHEN** the user runs `braveduck --db /tmp/test.db list`
- **THEN** braveduck reads and writes only `/tmp/test.db` and the files beside it

### Requirement: R-DAT02 Owner-only files
The data dir SHALL be created with owner-only access (0700), and the default `~/.braveduck` SHALL be tightened to 0700 if an older version left it wider. The database files, the profile, CV files, backups and key files SHALL be readable only by the user (0600).

#### Scenario: Old install
- **WHEN** braveduck opens a default data dir that is 0755
- **THEN** it changes the folder to 0700

### Requirement: R-DAT03 Schema upgrades
Opening a database made by an older braveduck SHALL upgrade its schema in place, automatically and in order. A shipped schema step MUST never be changed; changes come as new steps.

#### Scenario: Upgrade
- **WHEN** a newer braveduck opens an older database
- **THEN** the missing schema steps run and the user's jobs are kept

### Requirement: R-DAT04 Backups
While the board runs, braveduck SHALL copy the database once a day into the data dir's `backups/` folder and keep the newest 7 daily copies. `braveduck backup` SHALL make an extra copy on demand that is never pruned. A copy SHALL be written under a temporary name and renamed when complete, so a failed copy never looks like a backup. To restore, the user stops the board and copies a backup over the database.

#### Scenario: Eighth day
- **WHEN** the board makes its eighth daily backup
- **THEN** the oldest daily copy is deleted and manual copies stay

### Requirement: R-DAT05 Export
`braveduck export` (and the board's export) SHALL write every job as CSV, or with `--json` as full JSON records, to stdout or to the file named by `--out`.

#### Scenario: Spreadsheet
- **WHEN** the user runs `braveduck export --out jobs.csv`
- **THEN** jobs.csv holds one row per job with id, title, company, lane, score and the other listed columns

### Requirement: R-DAT06 Doctor
`braveduck doctor` SHALL report the Go runtime, the database path and whether it opens (with its schema version), the job count, the rater configuration, the profile's state, the getting-started steps, where the decision-model key comes from (never the key), where the session key comes from and each saved login's status, missing browser system libraries with the command to install them, and whether the board is running on its default address.

#### Scenario: Board not running
- **WHEN** the user runs `braveduck doctor` while `braveduck serve` is stopped
- **THEN** doctor says the board isn't running, so crawls, rating, the sweep and backups don't happen

### Requirement: R-DAT07 Pure-Go build
braveduck SHALL build and run as one binary without cgo: SQLite, the keychain and every other dependency MUST work as pure Go, and the board's built files SHALL be embedded so building braveduck needs no Node.

#### Scenario: Install
- **WHEN** the user runs `go install github.com/khangpworking/braveduck-cli/cmd/braveduck@latest` with Go 1.25+ and no C compiler
- **THEN** the binary builds and serves the board
