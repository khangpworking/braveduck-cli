// Live updates: the server sends "changed" on /api/events whenever the
// database changes (any face: board, CLI, MCP, crawls), and every query
// refetches. EventSource reconnects by itself; when it gives up, one probe
// says why: a restarted server (401: the page's token is dead), a server
// that's down (try again in a few seconds), or one that can't watch its
// database (refetch every 30 s instead).
import type { QueryClient } from '@tanstack/react-query'
import { onUnauthorized, token } from './api'

export function startLive(qc: QueryClient) {
  let es: EventSource | null = null
  let timer = 0
  const refresh = () => void qc.invalidateQueries()

  const open = () => {
    es = new EventSource(`/api/events?token=${encodeURIComponent(token)}`)
    es.addEventListener('changed', refresh)
    es.onerror = () => {
      if (es?.readyState !== EventSource.CLOSED) return
      fetch('/api/jobs', { headers: { Authorization: `Bearer ${token}` }, cache: 'no-store' }).then(
        res => {
          if (res.status === 401) onUnauthorized()
          else timer = window.setInterval(refresh, 30_000)
        },
        () => (timer = window.setTimeout(open, 3_000)),
      )
    }
  }
  open()
  return () => {
    es?.close()
    clearInterval(timer)
    clearTimeout(timer)
  }
}
