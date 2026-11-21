# Design

## Context

braveduck has two decision models that rate on the board itself: Jev (TypeSafe) and Clef
(Cloudflare Workers AI). The rating model a user rates with is one saved setting; when it
is not saved, the code reads it as Jev. A Jev key can come from the server (environment or
key file) or be pasted by the user. A Clef token is only ever pasted by the user, together
with a Cloudflare account ID.

Automatic rating (R-RAT05) writes a rater configuration the first time it turns a decision
model on. After that, a board where Jev turned itself on looks the same in storage as a
board where the user picked Jev.

## Goals / Non-Goals

**Goals:**
- A user who has not picked a rating model gets Clef (`clef-flash`).
- No board that rates with Jev today stops rating or changes rater by itself after the
  upgrade.

**Non-Goals:**
- A server-side Clef key.
- Changing grades, keys, caps or the opt-out.
- Removing Jev.

## Decisions

1. **"Not picked" reads as Clef, unless the saved raters include Jev.** The rating model
   is: the saved setting when there is one; else Jev when the saved rater configuration
   contains Jev's model id; else Clef. This is worked out on read, so no migration and no
   schema change. Alternative considered: a migration that writes "jev" on every existing
   database. Rejected: it would also mark fresh, unused boards as Jev.
2. **Jev still turns itself on when it has the only key.** With no Clef token and a Jev
   key available (the usual state of a shared server), automatic rating enables Jev, as
   today. Alternative considered: rate with nothing until a Clef token is pasted.
   Rejected for now: every user of a shared server would lose automatic rating, which
   R-RAT05 says is on by default.
3. **Clef wins when both keys are there and nothing was picked.** This is the case the
   owner's decision changes.
4. **The full-profile rule is model-neutral.** The same text goes to either model
   (R-RAT08 already asks both the same questions), so R-RAT04 says "the decision model".

## Risks / Trade-offs

- A board where Jev turned itself on keeps Jev even after its user pastes a Clef token,
  until the user switches in Settings → Rating → the rating window should say which model
  is rating, so the switch is easy to find.
- On a shared server with a server Jev key, most users will still start on Jev, because
  Clef has no server key → accepted; see the open questions.

## Migration Plan

No data migration. Ship as a normal release. Rollback: revert the commit; the saved
setting of anyone who switched stays valid in the old code.

## Open Questions

1. With only a Jev key and nothing picked, should Jev rate (decision 2, recommended), or
   should nothing rate until a Clef token is pasted?
2. Should saving a Clef token on a board where Jev turned itself on (and the user never
   picked a rater) switch that board to Clef by itself? Recommended: no, because storage
   cannot tell that board from one where the user chose Jev.
