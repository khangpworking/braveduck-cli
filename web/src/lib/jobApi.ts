// What the board does to one job beyond moving it: assisted apply, follow-ups,
// pins, notes and edits, ratings, tailored CVs and email drafts.
import { api, token, type Job, type JobPatch } from './api'
import type * as service from './gen/service'
import type * as wire from './gen/api'

export type Platform = service.PlatformStatus
export type RatingConfig = wire.RatingConfig
export type ApplyPrep = service.ApplyPrep
/** What to paste into an AI: a request for an agent with braveduck, or a full brief for any chat. */
export type Ask = wire.AskReply
export type RatingRequest = service.RatingRequest
export type Draft = service.Draft

export const getPlatforms = () => api<Platform[]>('GET', '/platforms')
export const getRatingConfig = () => api<RatingConfig>('GET', '/rating-config')

export const edit = (id: number, p: JobPatch) => api<Job>('PATCH', `/jobs/${id}`, p)
export const togglePin = (id: number) => api<Job>('POST', `/jobs/${id}/pin`)
export const toggleKeep = (id: number) => api<Job>('POST', `/jobs/${id}/keep`)
export const remove = (id: number) => api<unknown>('DELETE', `/jobs/${id}`)
export const followedUp = (id: number) => api<Job>('POST', `/jobs/${id}/followup`)

export const startApply = (id: number) => api<ApplyPrep>('POST', `/jobs/${id}/apply`)
export const confirmApplied = (id: number, submitted: boolean) =>
  api<Job>('POST', `/jobs/${id}/apply/confirm`, { submitted })

export const askRating = (id: number, model = '') => api<RatingRequest>('POST', `/jobs/${id}/rate`, { model })
export const askCV = (id: number) => api<Ask>('POST', `/jobs/${id}/cv`, {})
export const askDraft = (id: number, notes: string) => api<Ask>('POST', `/jobs/${id}/draft`, { notes })
export const saveCV = (id: number, markdown: string) => api<Job>('PUT', `/jobs/${id}/cv`, { markdown })
export const saveDraft = (id: number, d: Partial<Draft>) => api<Job>('PUT', `/jobs/${id}/draft`, d)
export const draftSent = (id: number) => api<Job>('POST', `/jobs/${id}/draft/sent`)
export const draftReject = (id: number) => api<Job>('POST', `/jobs/${id}/draft/reject`)

/** The email draft, or null when there is none (or it can't be read). */
export function draftOf(j: Job): Draft | null {
  if (!j.draft_email) return null
  try {
    const d = JSON.parse(j.draft_email) as Partial<Draft>
    return { to: d.to ?? '', subject: d.subject ?? '', body: d.body ?? '' }
  } catch {
    return null
  }
}

/**
 * Splits what an AI wrote into the email's subject and body: a first line
 * "Subject: …" is the subject, the rest is the body.
 */
export function splitEmail(text: string): Pick<Draft, 'subject' | 'body'> {
  const m = /^\s*Subject:\s*(.*)(?:\r?\n|$)/i.exec(text)
  return m ? { subject: m[1].trim(), body: text.slice(m[0].length).trim() } : { subject: '', body: text.trim() }
}

/** The connected job site whose login can apply to this job, if any. */
export function applySite(j: Job, platforms: Platform[]): Platform | undefined {
  let host = ''
  try {
    host = new URL(j.url).hostname.toLowerCase()
  } catch {
    return undefined
  }
  return platforms.find(
    p => p.status === 'connected' && p.domains.some(d => host === d || host.endsWith('.' + d)),
  )
}

/** A mailto: link that mail apps read the same way (spaces as %20, not +). */
export function mailto(d: Draft) {
  const q = new URLSearchParams({ subject: d.subject, body: d.body }).toString().replace(/\+/g, '%20')
  return `mailto:${encodeURIComponent(d.to).replace(/%40/g, '@')}?${q}`
}

/** Downloads a file the API serves (it needs the token, so a plain link won't do). */
export async function download(path: string, fallbackName: string) {
  const res = await fetch('/api' + path, { headers: { Authorization: `Bearer ${token}` } })
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: string }
    throw new Error(body.error ?? `HTTP ${res.status}`)
  }
  const cd = res.headers.get('Content-Disposition') ?? ''
  const name = decodeURIComponent(/filename\*=UTF-8''([^;]+)/i.exec(cd)?.[1] ?? /filename="?([^";]+)/i.exec(cd)?.[1] ?? fallbackName)
  const url = URL.createObjectURL(await res.blob())
  const a = Object.assign(document.createElement('a'), { href: url, download: name })
  a.click()
  setTimeout(() => URL.revokeObjectURL(url), 10_000)
}
