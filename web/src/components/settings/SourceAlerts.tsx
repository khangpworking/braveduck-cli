// A source that needs a look says so on the board itself, not only inside
// Settings: a scanner that stopped (fb-watcher) must not go unnoticed. Sites
// braveduck checks itself get Try again; another tool's source (Facebook's
// scanner) can't be run from here, so it points to Job sources instead.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, RotateCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Q, S, openWindow } from '@/lib/settings'
import { ago, unhealthyText, type SourceHealth } from '@/lib/schedule'
import { sourceName } from '@/lib/jobs'
import { locale } from '@/lib/i18n'
import { N_ } from '@/lib/n'
import { Button } from '@/components/ui/button'
import { SiteIcon } from '@/components/SiteIcon'
import { useMsg } from './parts'

// Background tasks have no site name of their own.
const TASKS: Record<string, string> = { 'topcv-sync': N_('TopCV updates'), 'jev-rate': N_('Rating') }

/** A source's name in words, from its schedule if the server sent one. */
export function useSourceName() {
  const { t } = useTranslation()
  const scheds = useQuery({ queryKey: Q.schedule, queryFn: S.schedule }).data
  return (id: string) => {
    const sc = scheds?.find(s => s.source === id)
    if (sc) return t(sc.name)
    return TASKS[id] ? t(TASKS[id]) : t(sourceName(id))
  }
}

/** The sources that need a look, with what went wrong and what to do. */
export function SourceWarnings({ here = false }: { here?: boolean }) {
  const { t } = useTranslation()
  const msg = useMsg()
  const qc = useQueryClient()
  const name = useSourceName()
  const status = useQuery({ queryKey: Q.crawlStatus, queryFn: S.crawlStatus }).data
  const scheds = useQuery({ queryKey: Q.schedule, queryFn: S.schedule }).data
  const retry = useMutation({
    mutationFn: S.crawl,
    onSuccess: (r, source) => {
      if (!r.ok) toast.error(t("Couldn't check {source}", { source: name(source) }))
      else toast.success(found(r.result?.added ?? 0))
    },
    onError: (e, source) =>
      /already being crawled/.test(e.message) ? toast(t('{source} is being checked already.', { source: name(source) })) : toast.error(t("Couldn't check {source}", { source: name(source) })),
    onSettled: () => {
      for (const k of [Q.crawlStatus, Q.schedule, ['jobs']]) void qc.invalidateQueries({ queryKey: k })
    },
  })
  const found = (n: number) => (n === 0 ? t('No new jobs right now.') : n === 1 ? t('Found 1 new job.') : t('Found {n} new jobs.', { n: n.toLocaleString(locale()) }))
  const bad = (status?.sources ?? []).filter(s => !s.healthy)
  if (!bad.length) return null
  const ours = (id: string) => !!scheds?.find(sc => sc.source === id)?.builtin
  return (
    <div className="space-y-2" data-warnings>
      {bad.map((s: SourceHealth) => (
        <div key={s.name} role="alert" data-warning={s.name} className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg border border-warning-border bg-warning-muted px-3 py-2 text-warning-strong">
          <AlertTriangle className="size-5 shrink-0 text-warning" />
          <span className="flex min-w-48 flex-1 items-center gap-1.5" title={s.lastError ? t(s.lastError) : undefined}>
            <SiteIcon id={s.name} />
            {t(unhealthyText(s), { source: name(s.name), when: s.lastRun ? msg(ago(s.lastRun)) : t('never') })}
          </span>
          {ours(s.name) ? (
            <Button variant="outline" className="h-10" disabled={retry.isPending} onClick={() => retry.mutate(s.name)}>
              <RotateCw />
              {retry.isPending && retry.variables === s.name ? t('Checking…') : t('Try again')}
            </Button>
          ) : (
            !here && (
              <Button variant="outline" className="h-10" onClick={() => openWindow('sources')}>
                {t('Job sources')}
              </Button>
            )
          )}
        </div>
      ))}
    </div>
  )
}
