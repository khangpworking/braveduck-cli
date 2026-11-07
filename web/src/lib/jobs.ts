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
  A: { label: N_('Great fit'), tone: 'bg-emerald-600 text-white' },
  B: { label: N_('Good fit'), tone: 'bg-teal-700 text-white' },
  C: { label: N_('Okay fit'), tone: 'bg-amber-500 text-white' },
  D: { label: N_('Weak fit'), tone: 'bg-orange-600 text-white' },
  F: { label: N_('Poor fit'), tone: 'bg-red-700 text-white' },
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

/** Saved jobs: pinned first, then best rated, unrated after, newest first. */
export function sortJobs(list: Job[]) {
  const rank = (j: Job) => (j.score_status === 'scored' ? Number(j.score) : -1)
  const time = (j: Job) => Date.parse(j.saved_at || j.created_at) || 0
  return [...list].sort(
    (a, b) => Number(b.pinned) - Number(a.pinned) || rank(b) - rank(a) || time(b) - time(a),
  )
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
  }
  return [known[d] ?? d]
}

/** The posting's link, if it's a web link: job text is hostile, and javascript: or data: links mustn't become clickable. */
export const postingUrl = (j: Job) => (/^https?:\/\//i.test(j.url.trim()) ? j.url.trim() : '')
