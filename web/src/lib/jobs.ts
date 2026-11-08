// How jobs read on the board: stage names, grades, pay, distance, and what to
// do today. Pure functions over the API's Job, so they're easy to test.
import type { Job, Status, TodayGroup } from './api'
import { N_ } from './n'

export const STAGES: { id: Status; name: string }[] = [
  { id: 'saved', name: N_('Saved') },
  { id: 'waiting', name: N_('Ready to apply') },
  { id: 'applied', name: N_('Applied') },
  { id: 'interview', name: N_('Interview') },
  { id: 'offer', name: N_('Offer') },
  { id: 'closed', name: N_('Closed') },
]

export const stageName = (s: Status) => STAGES.find(x => x.id === s)?.name ?? s

/** Still to apply for: the stages before Applied. */
export const isOpen = (j: Job) => j.status === 'saved' || j.status === 'waiting'

export type Grade = 'A' | 'B' | 'C' | 'D' | 'F' | '?'

export const GRADES: Record<Grade, { label: string; tone: string }> = {
  A: { label: N_('Great fit'), tone: 'bg-grade-a text-white' },
  B: { label: N_('Good fit'), tone: 'bg-grade-b text-white' },
  C: { label: N_('Okay fit'), tone: 'bg-grade-c text-white' },
  D: { label: N_('Weak fit'), tone: 'bg-grade-d text-white' },
  F: { label: N_('Poor fit'), tone: 'bg-grade-f text-white' },
  '?': { label: N_('Not rated yet'), tone: 'bg-muted text-muted-foreground' },
}

export function grade(j: Job): Grade {
  if (j.score_status !== 'scored') return '?'
  return (['A', 'B', 'C', 'D', 'F'] as const).find(g => g === j.score_letter) ?? '?'
}

/** The rater's reasons: a few points separated by " • ". */
export const reasons = (j: Job) =>
  j.score_reasoning.split(/\s+•\s+|\n+/).map(s => s.trim()).filter(Boolean)

/** Fold accents and case, so "ke toan" finds "Kế toán". */
export const fold = (v: string) =>
  v.normalize('NFD').replace(/[\u0300-\u036f]/g, '').replace(/đ/gi, 'd').toLowerCase()

export function matches(j: Job, query: string) {
  const hay = fold([j.title, j.company, j.location, j.skills, j.salary, j.notes, j.discovery_source].join(' '))
  return fold(query).split(/\s+/).filter(Boolean).every(w => hay.includes(w))
}

/**
 * Pay as people say it: "16000000 - 22000000 VND/month" → "16–22 triệu/tháng".
 * Anything else is shown as written; nothing at all is "".
 */
export function pay(salary: string, locale: string) {
  const s = salary.trim()
  if (!/vnd/i.test(s) || !/\d{6,}/.test(s)) return s
  const m = (n: string) => Number((Number(n) / 1e6).toFixed(1)).toLocaleString(locale)
  return s
    .replace(/(\d{6,})\s*-\s*(\d{6,})/, (_, a: string, b: string) => `${m(a)}–${m(b)}`)
    .replace(/\d{6,}/g, n => m(n))
    .replace(/\s*VND\s*\/\s*month/i, ' triệu/tháng')
    .replace(/\s*VND\s*\/\s*year/i, ' triệu/năm')
    .replace(/\s*VND\b/i, ' triệu')
}

/** No figure: empty, or a word like thỏa thuận / negotiable. */
export const payUndisclosed = (salary: string) =>
  /^$|thoa thuan|thuong luong|canh tranh|negotiable|competitive/.test(fold(salary).trim())

/** Kilometres from home, when the company's address was found. */
export const km = (j: Job) =>
  j.company_geo_status === 'ok' ? Math.round(j.company_distance_km) : null

/** Drops the legal form, so rows show "Hoa Sen Vàng", not "Công ty TNHH Hoa Sen Vàng". */
export const shortCompany = (c: string) =>
  c
    .replace(/^\s*(công ty|cty)\s+(tnhh|cổ phần|cp)(\s+(một|1)\s+thành viên|\s+mtv)?\s+/i, '')
    .replace(/^\s*(công ty|cty)\s+/i, '')
    .trim() || c

/** How a list can be ordered. Pinned jobs always come first. */
export type SortKey = 'fit' | 'newest' | 'pay' | 'near' | 'deadline'

export const SORTS: { id: SortKey; name: string }[] = [
  { id: 'fit', name: N_('Best fit') },
  { id: 'newest', name: N_('Newest') },
  { id: 'pay', name: N_('Highest pay') },
  { id: 'near', name: N_('Nearest') },
  { id: 'deadline', name: N_('Closing soonest') },
]

/**
 * The top of the pay, in millions of đồng a month, when it can be read:
 * "16000000 - 22000000 VND/month" → 22, "15-20 triệu" → 20, "$1,500" → null.
 */
export function payTop(salary: string): number | null {
  const s = fold(salary)
  if (/usd|\$/.test(s)) return null
  const big = [...s.matchAll(/\d{6,}/g)].map(m => Number(m[0]) / 1e6)
  if (big.length) return Math.max(...big)
  if (/trieu|tr\b/.test(s)) {
    const small = [...s.matchAll(/\d+(?:[.,]\d+)?/g)].map(m => Number(m[0].replace(',', '.')))
    if (small.length) return Math.max(...small)
  }
  return null
}

/** Days until the posting closes, or null when there's no deadline. */
const closesIn = (j: Job) => (j.clock?.kind === 'deadline' ? j.clock.days : null)

/**
 * Orders a list: pinned first, then by key, then newest. Jobs with nothing
 * to sort by (no pay given, no address, no deadline) go last.
 */
export function sortJobs(list: Job[], key: SortKey = 'fit') {
  const time = (j: Job) => Date.parse(j.saved_at || j.created_at) || 0
  const last = (v: number | null, desc = false) => (v === null ? Infinity : desc ? -v : v)
  const by: Record<SortKey, (j: Job) => number> = {
    fit: j => (j.score_status === 'scored' ? -Number(j.score) : Infinity),
    newest: j => -time(j),
    pay: j => last(payTop(j.salary), true),
    near: j => last(km(j)),
    deadline: j => last(closesIn(j)),
  }
  return [...list].sort((a, b) => Number(b.pinned) - Number(a.pinned) || by[key](a) - by[key](b) || time(b) - time(a))
}

export interface Step {
  stage: Status | 'followup'
  date: string // "" when braveduck doesn't know when it happened
}

/**
 * The steps a job went through, as far as braveduck knows: saved, applied
 * (the day the no-answer clock started), followed up, and the stage it is
 * in now. Dates braveduck doesn't have stay empty rather than guessed.
 */
export function steps(j: Job): Step[] {
  const order: Status[] = ['saved', 'waiting', 'applied', 'interview', 'offer', 'closed']
  const at = order.indexOf(j.status)
  const out: Step[] = [{ stage: 'saved', date: j.saved_at || j.created_at }]
  const applied = /^applied:(\d{4}-\d{2}-\d{2})/.exec(j.expires_at)?.[1] ?? ''
  if (at >= order.indexOf('applied') || applied) out.push({ stage: 'applied', date: applied })
  if (j.followup_sent_at) out.push({ stage: 'followup', date: j.followup_sent_at })
  if (j.status === 'interview' || j.status === 'offer') out.push({ stage: 'interview', date: '' })
  if (j.status === 'offer') out.push({ stage: 'offer', date: '' })
  if (j.status === 'closed') out.push({ stage: 'closed', date: '' })
  return out
}

export type TodayKind = 'follow' | 'deadline' | 'match'
export interface TodayEntry {
  job: Job
  kind: TodayKind
}

/**
 * What to do today, most urgent first: applications waiting a week for an
 * answer, then postings closing this week, then the best matches. Each job
 * shows once. The server picks the jobs (GET /api/today); the board only
 * orders them.
 */
export function todayEntries(groups: TodayGroup[], jobs: Job[]): TodayEntry[] {
  const byId = new Map(jobs.map(j => [j.id, j]))
  const order: [TodayGroup['kind'], TodayKind][] = [
    ['follow_up', 'follow'],
    ['deadlines', 'deadline'],
    ['best_matches', 'match'],
  ]
  const seen = new Set<number>()
  const out: TodayEntry[] = []
  for (const [kind, as] of order) {
    for (const it of groups.find(g => g.kind === kind)?.items ?? []) {
      const job = byId.get(it.job_id)
      if (!job || seen.has(job.id)) continue
      seen.add(job.id)
      out.push({ job, kind: as })
    }
  }
  return out
}

const SOURCES: Record<string, string> = {
  vietnamworks: 'VietnamWorks',
  careerlink: 'CareerLink',
  topcv: 'TopCV',
  joboko: 'JobOKO',
  facebook: 'Facebook',
  paste: N_('Added by you'),
}

/** A source's name as people know it ("topcv" → "TopCV"). */
export const sourceName = (s: string) => SOURCES[s.toLowerCase()] ?? s

/**
 * The server's deal-breaker labels are short Vietnamese tags ("Làm T7");
 * these are the words the board shows for them.
 */
export function dealbreakerText(d: string): [string, Record<string, string | number>?] {
  const far = /^Xa (\d+)\s*km$/i.exec(d)
  if (far) return [N_('{n} km away'), { n: Number(far[1]) }]
  const known: Record<string, string> = {
    'Làm T7': N_('Works on Saturdays'),
    'Lương thỏa thuận': N_('Pay not stated'),
    'Cty <2 năm': N_('Company under 2 years old'),
    'Cty ngừng hoạt động': N_('Company has stopped operating'),
  }
  return [known[d] ?? d]
}

/** The company's page in the business register (or its search), only if it's really there. */
export const registerUrl = (j: Job) => (j.company_info_url.startsWith('https://dulieuphapluat.vn/') ? j.company_info_url : '')

/** The posting's link, if it's a web link: job text is hostile, and javascript: or data: links mustn't become clickable. */
export const postingUrl = (j: Job) => (/^https?:\/\//i.test(j.url.trim()) ? j.url.trim() : '')
