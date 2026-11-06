# Spec: rating with Jev

Status: **built** · Owner choice (2026-11-04): Jev is the **default rater**,
using the **full profile**. Owner choice (2026-11-06): automatic Jev rating is
**on by default**, with a notice and a **per-user opt-out**.

## What Jev is

Jev (`jev-1.13.0`, `https://api.typesafe.ai/v1/systemone`) is TypeSafe AI's
decision model. It answers **multiple-choice questions** about a text
(`state`) and returns a choice plus a probability for every option. It doesn't
write text, so CVs and emails stay with the user's own AI.

## How braveduck rates with it

- **State:** the user's profile (template scaffolding stripped), then the job:
  title, company, location, salary, skills, description.
- **Questions:**
  - `fit` A–F, with criteria from the rubric bands;
  - `skills` strong / partial / weak / unclear;
  - `role` on_target / adjacent / off_target / unclear;
  - `seniority` match / above / below / unclear;
  - `pay` meets / below / unclear;
  - `dealbreaker` none / some.
- **Score:** Σ p(letter) × the band midpoint (A 4.75, B 4.2, C 3.7, D 3.2,
  F 2.0), rounded to 0.1. The letter is `LetterForScore(score)`, so the badge
  never disagrees with its number.
- **Reasons:** "Jev: C (57% sure) • skills: partial match • role: on target •
  …". "Unclear" answers are left out.
- The verdict goes through the same `SubmitRating` / `ValidateVerdict` as any
  rater. Jev is model id `jev-1.13.0` in the A/B config: as the default it
  owns the badge, and the user's AI can still rate as a challenger.

## When it runs

- **On by default:** when a Jev key is available and the user hasn't picked a
  rater yet, Jev becomes the default rater by itself (the scheduled job or the
  board's Rate button turns it on). A rater the user picked is kept.
- `braveduck jev enable` makes Jev the default rater and keeps the current
  raters as challengers.
- **Automatically:** the built-in source `jev-rate` in the universal schedule
  (every 30 min by default) rates up to 20 unrated jobs. It does nothing when
  there is no key, another rater was picked, or the user opted out, and stops
  at the first API error.
- On demand: `braveduck jev rate <id>|--all`, the board's Rate button, and
  MCP `rate_with_jev`.

## Privacy and the key

- **Sent to TypeSafe:** the user's full profile (their CV) and each job's
  text. The owner chose this over a condensed profile. `braveduck jev status`
  says so.
- **Notice:** the board's rating window (⚙ Settings → Rating) says in one
  line that the profile and jobs are sent to TypeSafe's Jev to rate them.
- **Opt-out:** in the same window, "Don't send my profile to Jev" stops Jev
  for that user: no automatic rating and no Jev rating on demand. The choice
  is saved in the user's own database, so on a shared server one user's
  opt-out never affects another. Unticking it turns Jev back on.
- **Key:** `TYPESAFE_API_KEY`, else the file `jev-route` uses
  (`~/.config/jev-route/typesafe.env`). It is read at call time, never
  printed, logged or stored, and redacted from errors (`Bearer ***`).

## Verified

On a throwaway DB with a synthetic profile, 7 live VietnamWorks jobs were
rated in ~2.5 s (≈0.4 s each). The two AI-automation roles came out C, and
the test-automation and senior roles F at 93–98%, with matching reasons.
