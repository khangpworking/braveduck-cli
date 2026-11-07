// The board's only way to the server: JSON over /api with the per-run token
// the Go server writes into the page (internal/api/api.go renderIndex).

export const token =
  globalThis.document?.querySelector<HTMLMetaElement>('meta[name="braveduck-token"]')?.content ?? ''

export type Status = 'saved' | 'waiting' | 'applied' | 'interview' | 'offer' | 'closed'

export interface Clock {
  kind: 'deadline' | 'applied'
  state: string // deadline: ok/warning/expired; applied: silent/countdown/expired
  days: number // deadline: days left; applied: days since sent
  date: string
  closes_in?: number
}

export interface Job {
  id: number
  title: string
  company: string
  url: string
  location: string
  status: Status
  pinned: boolean
  keep: boolean
  notes: string
  salary: string
  skills: string
  jd_summary: string
  jd_ai_summary: string
  jd_full: string
  discovery_source: string
  score: number
  score_letter: string
  score_status: string // "", pending, scored, failed
  score_reasoning: string
  dealbreakers?: string[]
  clock?: Clock
  ratings?: Record<string, { status: string; score: number; letter: string; reasoning: string }>
  apply_method: string // platform / email
  apply_state: string // "", filling, confirm, sent (queued/failed/replied: older values)
  apply_detail: string
  cv_status: string // "" or ready
  cv_version: number
  cv_generated_at: string
  draft_email: string // JSON {to, subject, body}, or ""
  draft_status: string // ready-to-review / sent / rejected
  draft_notes: string
  contact_email: string
  topcv_status: string // sent / seen / suitable / rejected
  vnw_status: string
  company_active_date: string
  expires_at: string
  followup_sent_at: string
  saved_at: string
  created_at: string
  updated_at: string
  company_distance_km: number
  company_geo_status: string
}

export interface TodayGroup {
  kind: 'best_matches' | 'deadlines' | 'follow_up'
  title: string
  items: { job_id: number; title: string; why: string }[]
}

export interface Setup {
  cv: boolean
  about_me: boolean
  wants: boolean
  search_queries: number
  location: boolean
  topcv: boolean
  done: boolean
  hosted?: unknown
}

export interface JobPatch {
  status?: Status
  pinned?: boolean
  keep?: boolean
  notes?: string
  title?: string
  company?: string
  salary?: string
  location?: string
}

/** An answer the server refused, with its message and any JSON it sent. */
export class ApiError extends Error {
  status: number
  body: Record<string, unknown>
  constructor(status: number, body: Record<string, unknown>) {
    super(typeof body.error === 'string' ? body.error : `HTTP ${status}`)
    this.status = status
    this.body = body
  }
}

/** Called when the server stops accepting our token (it restarted). */
export let onUnauthorized = () => {}
export function setOnUnauthorized(f: () => void) {
  onUnauthorized = f
}

export async function api<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch('/api' + path, {
    method,
    headers: {
      Authorization: `Bearer ${token}`,
      ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  let data: unknown = null
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    data = { error: text }
  }
  if (!res.ok) {
    if (res.status === 401) onUnauthorized()
    throw new ApiError(res.status, (data as Record<string, unknown>) ?? {})
  }
  return data as T
}

export const getJobs = () => api<Job[]>('GET', '/jobs')
export const getToday = () => api<TodayGroup[]>('GET', '/today')
export const getSetup = () => api<Setup>('GET', '/setup')
export const patchJob = (id: number, p: JobPatch) => api<Job>('PATCH', `/jobs/${id}`, p)
export const addContent = (content: string) => api<Job | { group: unknown; added: boolean }>('POST', '/jobs', { content })
