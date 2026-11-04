# Career Page — Functional Specification

> A feature/behavior inventory of the job-tracking "Career" page (`/learning`).
> Describes **what every element is and what it's for** — NOT how it currently
> looks. Use this to redesign the UI/UX freely while preserving all capability.

---

## 1. Purpose

A personal job-hunt command center built as a **Kanban board**. Jobs are
auto-discovered by crawlers (or pasted manually), scored by AI, tailored into
CVs/emails, and pushed through an application pipeline — all tracked on one
board. The user is a single person managing their own search.

---

## 2. Page-level chrome (top to bottom)

### 2.1 Header bar
- **Page title** ("braveduck").
- **Stat pills** — live counts: total `saved`, `applied`, `interview`, `offer`.
  Purpose: at-a-glance funnel size.
- **+ Add Job button** — toggles a drawer holding the paste form (2.4).

### 2.2 Today (with the interview-rate line)
- **Today** lists what to act on now, each item opening its card:
  - *best matches to apply for* — Saved/Waiting jobs rated A or B, best first (top 3);
  - *deadlines this week* — open jobs whose posting closes within 7 days;
  - *follow up* — jobs still in Applied ≥ 7 days after their applied date.
  With nothing to act on it says so.
- Underneath, the **interview rate** = interviews landed ÷ CVs that have had time
  to answer, with `N interviews from M answered applications · K waiting`.
- A **tiered encouragement message** whose tone escalates with the rate (below
  3 answered applications it says the rate shows once 3 have an answer).
- Rules that define it: a card is *resolved* once it reached interview/offer (a
  win) OR aged past a 14-day response window with no reply (a miss); freshly-sent
  CVs are *pending* (not counted, so they don't unfairly tank the rate). Needs
  ≥3 resolved before a rate is shown as meaningful.
- Purpose: motivational feedback loop, not a hard KPI.

### 2.3 Crawl status strip
- **"📥 N new jobs found · date"** headline with **per-source chips** (TopCV 3,
  LinkedIn 2, …). Purpose: did today's crawl actually bring anything in.
- **Unhealthy-crawler chips** — only crawlers that did NOT run today appear, each
  showing last-success date (or "never run") and a **Retry** button that
  re-triggers that crawler. A healthy crawler shows nothing (silence = fine).
- **Retry flash** — transient "⟳ Restarting X…" confirmation after a retry.

### 2.4 Add-Job drawer (paste form)
- A **textarea** to paste either a full JD (≥50 chars) or a job link.
- **Extract & Add** submits it; an AI (nanobot) extracts company, role, salary,
  contact, skills from the text/link and creates a card.
- **Result banners**: added (#id), already-in-tracker (duplicate), too-short,
  extraction-failed, fetch-failed (link blocked/expired → paste text instead).
- Drawer auto-opens when there's a result to show.

### 2.5 Board toolbar
- **Platform filter** — checkboxes (All + one per crawl source) that show/hide
  cards by source. Pure client-side visibility filter, persisted to
  localStorage. All real crawl sources always appear (even at 0 cards) so an
  empty source reads as "wired up, nothing today"; paste/other appear only when
  present. NOT a crawler on/off switch.
- **Search box** — filters cards by title, company, location, skills, salary
  and notes as you type (accents optional); `/` focuses it, Esc clears it.
- **Keyboard:** `n` opens Add Job, `?` lists the shortcuts, Esc closes the open
  window or drawer (not while writing in a text area).
- **📄 My CV** and **👤 Profile** — upload the base CV; edit the profile jobs are
  rated against.
- **⚙ Settings ▾** — a menu for the less-used windows: search terms & filters
  (the crawler keyword editor, 8.1), sources & schedule, rating (6.3),
  job-site logins, location, and **🔔 Notify me of great matches** (opt-in:
  while the board is open, a job newly rated A or B shows a desktop
  notification that opens its card; at most 3 at a time, then a "N more"
  summary; matches already rated when it's switched on don't notify).
  Arrow keys move through it, Esc closes it.

### 2.6 System activity log (bottom)
- A rolling feed of backend events (rater pokes, apply successes/failures) with
  timestamps and ✅/❌/🔄 icons. Auto-refreshes every 30s. Purpose: transparency
  into what the automation is doing.

---

## 3. The Kanban board

### 3.1 Stages (columns), left→right
`Saved → Waiting to apply → Applied → Interview → Offer → Closed`
- Each column shows a **live count**. Cards are **drag-and-drop** between columns
  to change status; moving also triggers stage-driven automation (see 5).
- On mobile, columns collapse to accordions (tap header to expand).
- On a desktop, a column **folds** to a narrow strip (name and count) so all
  six fit: Closed and empty columns start folded, a column unfolds once it has
  cards, and clicking a header folds/unfolds it for good. A folded strip still
  accepts dropped cards, and "Show" on a job unfolds its column.

### 3.2 Saved column extras
- **Sort control** — "Most recent" or "Highest score".
- **Pinned jobs** sort to the top with a **"N more" divider** separating them
  from the rest. Pinning only exists in Saved.

---

## 4. The Job Card

### 4.1 Collapsed face (always visible)
Every element and its purpose:

- **Dealbreaker pills** (red ⛔) — surface the user's hard no-gos without needing
  to expand. Four detectors: *Làm T7* (Saturday work, from JD text),
  *Lương thỏa thuận* (negotiable/undisclosed pay, from salary field),
  *Xa Nkm* (beyond the user's maximum commute, default 10 km; ignored past
  80 km as a bad geocode), *Cty <2 năm* (company founded <2 years ago). Only
  shown when detected. Computed once on the server (`jobs.Dealbreakers`) and
  sent with each job, so the board, `braveduck show` and MCP agree. Geo ones
  update live once a distance is known. (*Cty <2 năm* needs a company founding
  date, which braveduck can't look up yet.)
- **Job title** — links to the original posting in a new tab.
- **Score badge** — the default AI rater's verdict: letter grade (A–F) + numeric
  score, color-coded. States: `scoring…`, `score failed`, `stuck · retry`
  (clickable to re-score after 5 min hung), or the final grade.
- **Compare badges** — challenger models' grades (when A/B rating is on), each
  labeled with the model; shows `…` while pending, `✕` on failure.
- **Company name**.
- **Salary chip** (💰) — shown only when a concrete figure exists (negotiable pay
  is instead flagged as a dealbreaker).
- **Company intel badges** (mainly on pinned cards):
  - masothue cell: 📅 founding date (linked to tax-registry page), or 🏢 Unknown,
    or 🏢 … while looking up.
  - distance cell: ✅/🟡/🔴 + km from home (≤8 ideal, 8–10 ok, >10 too far), or
    📍 … while looking up, 📍 unknown / 📍 no address when it can't be placed.
    No badge until the user has set their location (nothing to measure from).
- **Footer row**:
  - age ("today" / "Nd") + platform source label + ✉ marker if email-apply.
  - **Expiry badge** (see 7).
  - **Auto-apply progress** (⏳ CV / email / sending) while automation runs.
  - **Apply-state badge** (🕒 queued, ✍ filling…, ✕ failed·retry, ↗ apply
    yourself, ⏳ cooldown m:ss) — the live status of the automated apply engine.
  - **Sent markers** (✓ sent / ✓ replied) and **📨 follow-up** when a follow-up
    email went out.
  - **Platform status badges** — TopCV recruiter status (👁 seen, 📋 reviewing,
    🤔 considering, ✅ suitable, ❌ rejected) and VNW (👁 seen).
  - **📌 pin marker**, **CV link** (labeled CV / CV v2…), **▾ expand toggle**.
- **Quick actions** (top): View ↗ (open posting), Pin/Unpin (Saved only), Delete.
- **Move-to-stage dropdown** — alternative to drag for changing status.
- **Green left border** when an application email has been sent.

### 4.2 Expanded detail panel (▾ toggles it)
- **AI assessment** — the default model's reasoning as bullet points.
- **Other models** — each challenger's grade + reasoning, plus an
  **"+ Ask another model…" dropdown + Ask button** to score this one card with
  any additional model on demand.
- **Skills** — extracted skill tags.
- **Summary** — AI-generated JD summary (bulleted) or raw summary.
- **Full JD** — collapsible, only for pasted jobs.
- **Notes** — freeform notes on the job.
- **Apply block** (see 5.2).
- **CV controls** — download CV + Re-draft (Waiting/Applied lanes only; CV
  drafting is lane-driven, not a button). Re-score button when scoring failed.

---

## 5. Application pipeline & automation

### 5.1 Lane-driven automation
- Moving a card to **"Waiting to apply"** auto-triggers: **CV tailoring** and (for
  email-apply jobs) **email drafting**. The card shows live "✍️ Tailoring CV…" /
  "Drafting email…" tickers (elapsed timer + ~1–2 min hint; "⚠️ Stalled" after
  15 min → a reaper retries).
- The card **auto-advances to Applied** once the application is actually sent.

### 5.2 Apply method block (in detail panel)
Two apply channels, auto-detected per job:
- **🌐 On-site apply** — jobs on any site the user has a saved login for
  (TopCV, VietnamWorks, JobOKO, CareerLink, or one added in platforms.json).
  **"Apply on <site>"** (shown once there's a CV to send: tailored, or the
  uploaded base CV) opens the job in a visible window with that login; the user
  submits there, braveduck never does. Afterwards TopCV jobs are confirmed from
  TopCV's application history; on other sites the card asks **"Did you submit
  it?"** — **Yes, I applied** moves it to Applied (starting the no-answer
  clock), **Not yet** clears the question. The apply-state badge (4.1) shows
  filling/failed; a daily cap and a cooldown between applies protect the account.
- **✉ Email apply** — jobs applied to by cold email. Shows an editable
  **recipient field** and the **draft email editor** (5.3).
- Past the Applied stage (Interview/Offer/Closed) there's nothing to apply to, so
  this block hides — except **interview-reply drafts**, which reuse the same
  editor to send a reply to a recruiter (no CV).

### 5.3 Email draft editor
- States: drafting / failed (Re-draft) / no-draft-yet (write notes, Draft email) /
  sending / sent (read-only, shows recipient) / ready-to-review.
- Review controls: **editable body**, Save edit, **Approve** (persists edits then
  sends), **Reject**, **Re-draft**, and a **draft-notes** box (tone/focus/
  availability hints that steer re-drafts).

---

## 6. AI rating system

- Every job is auto-scored (A–F + number) with reasoning.
- **Default model** owns the main score badge. **A/B mode** lets up to 3 models
  score simultaneously; challengers show as compare badges + detail reasoning.
- **Ask another model** — one-off score of a single card by any model.
- **Rating config panel** (⚙ Rating): enable A/B, pick models (max 3), choose the
  default. After enabling, a banner offers **"Rate all saved jobs"** (backfill)
  or **"Only new jobs from now."**
- Re-score on failure; a 5-min-stuck badge offers manual retry.

---

## 7. Deadlines & auto-clear

- **Expiry badge**, driven by two clocks:
  - **Real deadline** (Saved / Waiting cards with a scraped application
    deadline): grey date → amber "⚠ Nd left" (≤10 days) → red
    "⛔ to Closed in Nd" countdown once it closes.
  - **Apply-silence** (Applied cards, counted from the day the application was
    sent): grey "📮 Nd" while silent → red "⛔ to Closed in Nd" once 20 days pass.
    Interview / Offer cards have no clock.
- An hourly sweep (while the board runs) **moves** a card to **Closed** when its
  clock runs out — 7 days after the deadline, or 27 days after applying with no
  answer — with a note saying why ("Closed automatically: …"). Nothing is
  deleted; dragging the card back undoes it. **"Keep it"** (🛟 kept) exempts a
  card; **pinned** cards are also exempt (show "⛔ closed" / "⛔ no reply" with
  no countdown). Unknown-deadline cards show nothing.
- Purpose: keep the board fresh without silently losing anything (everything
  warns before it goes, and there are two escape hatches).

---

## 8. Config panels

### 8.1 Crawler keyword editor (⚙ Keywords)
- Edit the keyword lists that drive each crawler's searches.
- **Preview** shows what a keyword change would match before saving; **Save all**
  persists. Purpose: tune what jobs get crawled.

### 8.2 Rating config — see §6.

---

## 9. Real-time behaviors

- **Live cross-tab refresh** via SSE (`/events`): when any tab mutates the board,
  others update (with echo-suppression so the acting tab doesn't double-reload).
- **Score polling**: while any card is `scoring…`, poll `/jobs/statuses` and swap
  badges in place; mark `stuck` after 5 min.
- **Rating polling**: `/jobs/ratings` for challenger verdicts as they land.
- **Apply-state polling**: cooldown timers tick down live; failed→retry inline.
- **Company/geo twin**: masothue + distance + geo dealbreakers re-render live
  once the async lookup (fired on pin) resolves.
- **Draft tickers** and **activity log** refresh on intervals.

---

## 10. Actions / endpoints reference

| Action | Endpoint |
|---|---|
| Paste/extract a job | `POST /jobs/paste` |
| Change stage | `POST /jobs/:id/status` |
| Pin / unpin | `POST /jobs/pin` |
| Delete | `POST /jobs/delete` |
| Keep (exempt from auto-clear) | `POST /jobs/keep` |
| Generate / re-draft CV | `POST /jobs/generate-cv` |
| Approve CV (mark applied) | `POST /jobs/cv-approve` |
| Re-score | `POST /jobs/rescore` |
| Save contact email | `POST /jobs/contact-email` |
| Draft: edit/approve/reject/regenerate/notes | `POST /jobs/draft/*` |
| Ask another model | `POST /jobs/rate-add-model` |
| Backfill ratings | `POST /jobs/rate-backfill` |
| Rating config | `GET/POST /jobs/rating-config` |
| Keyword lists + preview | `/jobs/keywords`, `/jobs/keywords/preview` |
| Retry a crawler | `POST /learning/crawler/retry` |
| Retry a failed auto-apply | `POST /jobs/apply-retry` |
| Poll statuses / ratings | `GET /jobs/statuses`, `GET /jobs/ratings` |
| Live board events | SSE `/events`; log `GET /api/board-events` |

---

## 11. Key data per job (drives the UI)

`title, company, url, salary, location, skills, status (stage), pinned, keep,
score + score_letter + score_status + score_reasoning, per-model ratings,
jd_summary / jd_ai_summary / jd_full, discovery_source (platform), saved_at,
apply_method (email|on-site), apply_state (+ next_at, detail), cv_path +
cv_status + cv_version + cv_generated_at, draft_email + draft_status + notes,
contact_email, expires_at (deadline or apply-silence anchor), followup_sent_at,
topcv_status / vnw_status, company_active_date (masothue) + company_distance_km +
company_geo_status, notes.`

---

### Redesign notes
- Nothing above prescribes layout, color, or component style — all of that is
  open. The board metaphor (stages as a funnel) and the collapsed→expanded card
  progression are the two load-bearing interaction ideas; everything else is a
  feature that must remain reachable, not a layout that must be kept.
- Density matters to this user (many cards, many signals per card) — but how you
  handle that density is yours to reinvent.
