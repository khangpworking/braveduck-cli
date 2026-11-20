# company-and-location Specification

## Purpose
Facts braveduck adds to each job so the user can judge it at a glance: how far the workplace is from the user's own location, and what Vietnam's business register says about the company (its registered name, founding date and whether it still operates). Both are looked up politely in the background and feed the deal-breakers.

## Requirements

### Requirement: R-LOC01 Setting the user's location
The user SHALL be able to set their location from the browser's own location prompt, a click on the map, a typed address looked up with OpenStreetMap Nominatim, or coordinates, and clear it. A typed address search MUST be at least 3 characters and SHALL return up to 5 places, biased toward the current location. The location MUST NOT leave the machine except as that bias. Clearing the location SHALL forget every job's distance.

#### Scenario: Short search
- **WHEN** the user searches places for "HN"
- **THEN** the search is refused: type at least 3 characters

### Requirement: R-LOC02 Maximum commute
The maximum commute SHALL default to 10 km and MUST be between 1 and 500 km when set; jobs farther away get the "too far" deal-breaker (R-PIP10).

#### Scenario: Out of range
- **WHEN** the user sets a maximum commute of 600 km
- **THEN** the change is refused

### Requirement: R-LOC03 Job distances
Each job's distance SHALL be measured from the user's location to its workplace, using cached address lookups first and at most 20 new Nominatim lookups per pass, made a few at a time while the board runs. A lookup that fails for network reasons MUST NOT be cached, so it is tried again later. Without a user location, no distance SHALL be shown.

#### Scenario: Many new jobs
- **WHEN** a crawl adds 50 jobs with new addresses
- **THEN** 20 are looked up in this pass and the rest in later passes

### Requirement: R-LOC04 Business register lookups
While `braveduck serve` runs (and never otherwise), each open job's company SHALL be looked up once in the business register by exact legal name, using the workplace's province to tell same-name companies apart, and again only when the company is edited. Requests MUST be at least 2 seconds apart across the process, with at most 5 requests per lookup and 10 new names per pass. Answers SHALL be cached; trouble reaching the register leaves the job for a later pass.

#### Scenario: Name not resolved
- **WHEN** the register has no single company with the job's company name
- **THEN** the job keeps a link to the register's search for that name

### Requirement: R-LOC05 What the register adds to a job
A job whose company was found SHALL show the registered name, tax code, founding date, the register's status words and a link to the company's page. A company founded less than 2 years ago, or one the register lists as no longer operating, SHALL raise its deal-breaker (R-PIP10).

#### Scenario: Young company
- **WHEN** the register says the company started operating 14 months ago
- **THEN** the job carries the label "Cty <2 năm"
