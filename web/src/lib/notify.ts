// Great-match notifications: while the board is open, a job newly rated A or
// B pops a desktop notification. Opt-in, remembered per browser under the
// old board's keys; jobs already rated when it's switched on don't notify.
import type { Job } from './api'
import { isOpen } from './jobs'

export const NOTIFY_KEY = 'notify_matches'
export const NOTIFIED_KEY = 'notified_match_ids'

/** Rated A or B, and not applied for yet. */
export const greatMatches = (jobs: Job[]) =>
  jobs.filter(j => j.score_status === 'scored' && (j.score_letter === 'A' || j.score_letter === 'B') && isOpen(j))

/** The great matches not notified yet, best first. */
export const freshMatches = (jobs: Job[], seen: Set<number>) =>
  greatMatches(jobs)
    .filter(j => !seen.has(j.id))
    .sort((a, b) => Number(b.score) - Number(a.score))

function store(): Storage | null {
  try {
    return localStorage
  } catch {
    return null
  }
}

export const canNotify = () => 'Notification' in window

export const notifyOn = () =>
  store()?.getItem(NOTIFY_KEY) === 'on' && canNotify() && Notification.permission === 'granted'

export function seenIds(): Set<number> {
  try {
    return new Set(JSON.parse(store()?.getItem(NOTIFIED_KEY) || '[]') as number[])
  } catch {
    return new Set()
  }
}

export const markSeen = (jobs: Job[]) =>
  store()?.setItem(NOTIFIED_KEY, JSON.stringify(greatMatches(jobs).map(j => j.id)))

export const setNotify = (on: boolean) => store()?.setItem(NOTIFY_KEY, on ? 'on' : 'off')

/** At most this many jobs get their own notification after one refresh. */
export const MAX_NOTIFIED = 3
