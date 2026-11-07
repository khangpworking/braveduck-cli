// The settings windows' side of the API, and the names of the windows.
// Each endpoint has its own query key; a change invalidates the ones it
// touches (live updates refetch the rest).
import { ApiError, api, getSetup, onUnauthorized, token, type Setup } from './api'
import type { Money } from './money'
import type { CrawlStatus, SourceSchedule } from './schedule'
import type { ActivityEvent } from './activity'
import { fileName } from './windows'

export * from './windows'

export interface Hosted {
  user: string
  logout_url?: string
  owner?: boolean // the server runs on this user's computer
}

export const hostedOf = (s: Setup | undefined) => (s?.hosted as Hosted | undefined) ?? null

export interface ProfileForm {
  about_me: string
  roles: string[]
  seniority: string[]
  industries_want: string[]
  industries_avoid: string[]
  min_pay: Money
  target_pay: Money
  work_modes: string[]
  job_types: string[]
  deal_breakers: string[]
  languages: string[]
  notes: string
  [other: string]: unknown // the fields the form doesn't show go back as they came
}

export interface ProfileOptions {
  seniority: string[]
  work_modes: string[]
  job_types: string[]
  currencies: string[]
  periods: string[]
  deal_breakers: string[]
  languages: string[]
}

export interface Profile {
  path: string
  text: string
  filled: boolean
}

export interface BaseCV {
  name: string
  kind: string
  size: number
  uploaded_at: string
  profile_filled: boolean
}

export type Keywords = Record<string, string[]>

export interface RatingConfig {
  default_model: string
  ab_mode: boolean
  models: string[]
}

export interface Jev {
  available: boolean
  opt_out: boolean
}

export interface Platform {
  id: string
  name: string
  status: 'not_connected' | 'connecting' | 'connected' | 'expired' | 'unknown'
  account?: string
  builtin: boolean
}

export interface Location {
  set: boolean
  lat?: number
  lng?: number
  label?: string
  max_km: number
}

export interface Place {
  lat: number
  lng: number
  label: string
}

export interface FBGroup {
  key: string
  url: string
  name: string
  visibility: 'checking' | 'public' | 'private' | 'unavailable'
  note: string
}

export interface CrawlReply {
  ok: boolean
  message?: string
  result?: { added?: number }
}

export const Q = {
  setup: ['setup'],
  profileForm: ['profile-form'],
  profile: ['profile'],
  cv: ['cv'],
  keywords: ['keywords'],
  schedule: ['crawl-schedule'],
  crawlStatus: ['crawl-status'],
  rating: ['rating-config'],
  jev: ['jev'],
  platforms: ['platforms'],
  location: ['location'],
  agentKey: ['agent-key'],
  fbGroups: ['facebook-groups'],
  activity: ['activity'],
} as const

export const S = {
  setup: getSetup,
  profileForm: () => api<{ form: ProfileForm; options: ProfileOptions }>('GET', '/profile/form'),
  saveProfileForm: (form: ProfileForm) => api<{ form: ProfileForm; options: ProfileOptions }>('PUT', '/profile/form', { form }),
  profile: () => api<Profile>('GET', '/profile'),
  saveProfile: (text: string) => api<Profile>('PUT', '/profile', { text }),
  cv: () => api<{ cv: BaseCV | null; profile: { filled: boolean } }>('GET', '/cv'),
  keywords: () => api<Keywords>('GET', '/keywords'),
  saveKeywords: (k: Keywords) => api<Keywords>('PUT', '/keywords', k),
  schedule: () => api<SourceSchedule[]>('GET', '/crawl-schedule'),
  setSchedule: (source: string, every: number) =>
    api<SourceSchedule>('PUT', `/crawl-schedule/${encodeURIComponent(source)}`, { every_minutes: every }),
  crawlStatus: () => api<CrawlStatus>('GET', '/crawl-status'),
  crawl: (source: string) => crawlNow(source),
  rating: () => api<RatingConfig>('GET', '/rating-config'),
  saveRating: (c: RatingConfig) => api<RatingConfig>('PUT', '/rating-config', c),
  jev: () => api<Jev>('GET', '/jev').catch((): Jev => ({ available: false, opt_out: false })),
  setJev: (opt_out: boolean) => api<Jev>('PUT', '/jev', { opt_out }),
  platforms: () => api<Platform[]>('GET', '/platforms'),
  connect: (id: string) => api<Platform>('POST', `/platforms/${encodeURIComponent(id)}/login`),
  disconnect: (id: string) => api<{ deleted: boolean }>('DELETE', `/platforms/${encodeURIComponent(id)}/login`),
  sync: (id: string) =>
    api<{ found: number; updated: number; moved: number; added: number }>('POST', `/platforms/${encodeURIComponent(id)}/sync`),
  location: () => api<Location>('GET', '/location'),
  saveLocation: (p: Place & { max_km: number }) => api<Location>('PUT', '/location', p),
  forgetLocation: () => api<unknown>('DELETE', '/location'),
  places: (q: string) => api<Place[]>('GET', `/places?q=${encodeURIComponent(q)}`),
  agentKey: () => api<{ exists: boolean }>('GET', '/agent-key'),
  newAgentKey: () => api<{ key: string }>('POST', '/agent-key'),
  removeAgentKey: () => api<{ exists: boolean }>('DELETE', '/agent-key'),
  fbGroups: () => api<FBGroup[]>('GET', '/facebook-groups'),
  unfollow: (key: string) => api<unknown>('DELETE', `/facebook-groups/${encodeURIComponent(key)}`),
  activity: () => api<ActivityEvent[]>('GET', '/activity'),
}

/**
 * Runs a site's check now. A site that answers "couldn't" (200 ok:false, or
 * 501 for someone else's tool) is a reply, not an error.
 */
async function crawlNow(source: string): Promise<CrawlReply> {
  try {
    return await api<CrawlReply>('POST', `/crawl/${encodeURIComponent(source)}/retry`)
  } catch (e) {
    if (e instanceof ApiError && e.body.ok === false) return { ok: false, message: String(e.body.message ?? '') }
    throw e
  }
}

/** A request with the board's token that isn't JSON both ways. */
async function send(method: string, path: string, body?: BodyInit) {
  const res = await fetch('/api' + path, { method, headers: { Authorization: `Bearer ${token}` }, body })
  if (!res.ok) {
    if (res.status === 401) onUnauthorized()
    let data: Record<string, unknown> = {}
    try {
      data = (await res.json()) as Record<string, unknown>
    } catch {
      // not JSON: the status says enough
    }
    throw new ApiError(res.status, data)
  }
  return res
}

/** Uploads the CV; replace overwrites an "About me" the user wrote. */
export async function uploadCV(file: File, replace: boolean) {
  const form = new FormData()
  form.append('file', file)
  if (replace) form.append('replace', 'true')
  return (await send('POST', '/cv', form)).json() as Promise<BaseCV>
}

/**
 * Downloads a file from the API. A plain link can't send the token, so it's
 * fetched, then saved from a Blob.
 */
export async function download(path: string, fallback: string) {
  const res = await send('GET', path)
  const url = URL.createObjectURL(await res.blob())
  const a = Object.assign(document.createElement('a'), { href: url, download: fileName(res.headers.get('Content-Disposition'), fallback) })
  document.body.append(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 10_000)
}

