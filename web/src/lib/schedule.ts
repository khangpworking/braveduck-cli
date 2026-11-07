// Job sources: how often each one checks, and how long ago it last did.
// Pure, so it's easy to test; texts come back as [key, values] for t().
import type * as service from './gen/service'
import type * as store from './gen/store'
import { N_ } from './n'

export type Msg = [string, Record<string, string | number>?]

/** One source's schedule (GET /api/crawl-schedule). */
export type SourceSchedule = service.SourceSchedule
/** One source's health (GET /api/crawl-status). */
export type SourceHealth = store.CrawlSource
export type CrawlStatus = store.CrawlStatusView

export const EVERY: [number, string][] = [
  [0, N_('off')],
  [5, N_('every 5 minutes')],
  [15, N_('every 15 minutes')],
  [30, N_('every 30 minutes')],
  [60, N_('every hour')],
  [120, N_('every 2 hours')],
  [360, N_('every 6 hours')],
  [720, N_('every 12 hours')],
  [1440, N_('once a day')],
]

/** "every hour", or "every {n} minutes" for a value the list doesn't have. */
export function everyLabel(minutes: number): Msg {
  const c = EVERY.find(([m]) => m === minutes)
  return c ? [c[1]] : [N_('every {n} minutes'), { n: minutes }]
}

/**
 * The choices a source offers: off, and every interval it allows; its
 * current value stays on the list even when it isn't a usual one.
 */
export function intervalOptions(sc: Pick<SourceSchedule, 'every_minutes' | 'min_every_minutes'>): { minutes: number; label: Msg }[] {
  const out = EVERY.filter(([m]) => m === 0 || m >= sc.min_every_minutes).map(([m]) => m)
  if (!out.includes(sc.every_minutes)) out.push(sc.every_minutes)
  return out.sort((a, b) => a - b).map(m => ({ minutes: m, label: everyLabel(m) }))
}

/** A job site (rather than a background task or someone else's tool). */
export const isSite = (sc: Pick<SourceSchedule, 'builtin' | 'task'>) => sc.builtin && !sc.task

/** How long ago, in words: "just now", "5 minutes ago", "2 days ago". */
export function ago(iso: string, now = Date.now()): Msg {
  const min = Math.floor((now - Date.parse(iso)) / 60000)
  if (!(min >= 1)) return [N_('just now')]
  if (min < 60) return min === 1 ? [N_('{n} minute ago'), { n: 1 }] : [N_('{n} minutes ago'), { n: min }]
  const h = Math.floor(min / 60)
  if (min < 1440) return h === 1 ? [N_('{n} hour ago'), { n: 1 }] : [N_('{n} hours ago'), { n: h }]
  const d = Math.floor(min / 1440)
  return d === 1 ? [N_('{n} day ago'), { n: 1 }] : [N_('{n} days ago'), { n: d }]
}

/**
 * A source's last check, in words, and whether it went wrong. When is set,
 * it fills the text's {time}. A background task's own error is shown (it
 * says what to fix); a job site's isn't (it's the site's raw reply).
 */
export function sourceState(sc: SourceSchedule, h: SourceHealth | undefined, now = Date.now()): { text: Msg; when?: Msg; bad: boolean } {
  const site = isSite(sc)
  if (!h?.lastRun) return { text: [site ? N_('Not checked yet') : N_("Hasn't run yet")], bad: false }
  if (h.lastError) return { text: [site ? N_("The last check didn't work") : h.lastError], bad: true }
  return { text: [site ? N_('Last checked {time}') : N_('Last ran {time}')], when: ago(h.lastRun, now), bad: false }
}

/** A warning for a source that needs a look (the old board's Today strip). */
export function unhealthyText(h: SourceHealth): string {
  if (h.task) return N_("{source} isn't working (last try: {when})")
  if (h.lastError) return N_("{source} couldn't check for jobs (last try: {when})")
  return N_("{source} hasn't checked for jobs lately (last try: {when})")
}

/**
 * Sums up "Check now": the sites that failed (a site already being checked
 * by its schedule hasn't), and the jobs found.
 */
export function checkSummary(results: PromiseSettledResult<{ ok?: boolean; result?: { added?: number } }>[], names: string[]) {
  let added = 0
  const failed: string[] = []
  results.forEach((r, i) => {
    if (r.status === 'fulfilled') {
      added += r.value?.result?.added ?? 0
      if (!r.value?.ok) failed.push(names[i])
    } else if (!/already being crawled/.test(String((r.reason as Error)?.message ?? ''))) {
      failed.push(names[i])
    }
  })
  return { added, failed }
}
