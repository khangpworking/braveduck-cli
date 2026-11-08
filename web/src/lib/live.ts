// Live updates: the server sends "changed" on /api/events whenever the
// database changes (any face: board, CLI, MCP, crawls), and the board
// refetches what it shows. EventSource reconnects by itself; when it gives
// up, one probe says why: a restarted server (401: the page's token is dead),
// a server that's down (try again in a few seconds), or one that can't watch
// its database (refetch every 30 s instead).
//
// Changes come in bursts (a crawl adds twenty jobs, Jev rates them one by
// one), so they're gathered into one refetch a moment after the last; and a
// tab out of sight doesn't refetch at all until it's looked at again.
import type { QueryClient } from '@tanstack/react-query'
import { onUnauthorized, token } from './api'

/** How long a burst of changes is gathered before one refetch. */
export const GATHER_MS = 300

/**
 * Turns a stream of "something changed" into as few refetches as possible:
 * one per burst, none while hidden() says nobody's looking, one when they
 * look again if anything changed meanwhile.
 */
export function gatherer(refresh: () => void, hidden: () => boolean, wait = GATHER_MS) {
  let timer: ReturnType<typeof setTimeout> | undefined
  let missed = false
  const run = () => {
    timer = undefined
    if (hidden()) missed = true
    else refresh()
  }
  return {
    changed() {
      if (timer === undefined) timer = setTimeout(run, wait)
    },
    shown() {
      if (missed && !hidden()) {
        missed = false
        refresh()
      }
    },
    stop() {
      clearTimeout(timer)
    },
  }
}

export function startLive(qc: QueryClient) {
  let es: EventSource | null = null
  let timer = 0
  // Refetch what's on screen; the rest is marked stale and refetches when shown.
  const g = gatherer(() => void qc.invalidateQueries(), () => document.hidden)
  const onShown = () => g.shown()
  document.addEventListener('visibilitychange', onShown)

  const open = () => {
    es = new EventSource(`/api/events?token=${encodeURIComponent(token)}`)
    es.addEventListener('changed', () => g.changed())
    es.onerror = () => {
      if (es?.readyState !== EventSource.CLOSED) return
      fetch('/api/jobs?view=list', { headers: { Authorization: `Bearer ${token}` }, cache: 'no-store' }).then(
        res => {
          if (res.status === 401) onUnauthorized()
          else timer = window.setInterval(() => g.changed(), 30_000)
        },
        () => (timer = window.setTimeout(open, 3_000)),
      )
    }
  }
  open()
  return () => {
    es?.close()
    g.stop()
    document.removeEventListener('visibilitychange', onShown)
    clearInterval(timer)
    clearTimeout(timer)
  }
}
