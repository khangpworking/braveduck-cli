// The board's only way to the server: JSON over /api with the per-run token
// the Go server writes into the page (internal/api/api.go renderIndex).

import type * as jobs from './gen/jobs'
import type * as service from './gen/service'
import type * as wire from './gen/api'

export const token =
  globalThis.document?.querySelector<HTMLMetaElement>('meta[name="braveduck-token"]')?.content ?? ''

// The API's types are generated from the Go structs (tygo.yaml → gen/).
export type { Status } from './gen/jobs'
export type Clock = jobs.Clock
export type Job = jobs.Job
export type TodayGroup = service.TodayGroup
export type Setup = service.SetupStatus
export type JobPatch = wire.JobPatch

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
export const addContent = (content: string) => api<Job | wire.FollowGroupReply>('POST', '/jobs', { content })
