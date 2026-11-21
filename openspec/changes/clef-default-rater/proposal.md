# Proposal

## Why

On 2026-11-20 the owner decided that Clef (Cloudflare Workers AI) is braveduck's default
rating model, in place of Jev. Rule R-RAT04 still says "Jev SHALL be the default rater"
(owner choice of 2026-11-04), and the code still treats Jev as the rating model until the
user picks one. This change brings the rule in line with the newer decision.

## What Changes

- **R-RAT04** names Clef (`clef-flash`) as the rating model of a user who has not picked
  one. Today that is Jev.
- A board whose saved raters already include Jev keeps Jev until the user switches
  (one click in Settings → Rating, as R-RAT09 already allows). Nothing changes on such a
  board by itself.
- **R-RAT05** (automatic rating on by default) says which decision model takes the badge
  on a board where no rater was picked: Clef when a Clef token is saved; Jev only when a
  Jev key is the only key there is.
- The rule that the full profile is sent to the decision model stays as it is; it now
  reads for either model.
- The board's rating window, `braveduck jev status`, the README and
  `docs/specs/jev-rating.md` stop calling Jev the default.

Out of scope: how a grade is computed (R-RAT08), keys (R-RAT10), the daily cap (R-RAT11),
the opt-out (R-RAT06), and a server-side Clef key (Clef still needs the user's own token).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `rating`: R-RAT04 (which decision model is the default) and R-RAT05 (which decision
  model turns itself on when no rater was picked).

## Impact

- `internal/service` (decision settings, automatic enabling, the scheduled `jev-rate`
  run); no schema change.
- The board: the default-rater dropdown and rating window in `web/`, the rebuilt
  embedded board in `internal/api/app`, and `web/src/locales/vi.json`.
- CLI text of `braveduck jev status`; README and `docs/specs/jev-rating.md`.
- No new dependency. No change to what is sent or stored.
