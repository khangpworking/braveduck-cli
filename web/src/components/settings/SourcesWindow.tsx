// Job sources: the job sites up front, each with how often it checks and
// how its last check went, and one "Check now". Background tasks and other
// tools' sources wait under Advanced. A source that needs a look says so
// at the top, with Try again.
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, ChevronDown, RotateCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Q, S, openWindow } from '@/lib/settings'
import { ago, checkSummary, everyLabel, intervalOptions, isSite, sourceState, unhealthyText, type SourceHealth, type SourceSchedule } from '@/lib/schedule'
import { sourceName } from '@/lib/jobs'
import { locale } from '@/lib/i18n'
import { N_ } from '@/lib/n'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { cn } from '@/lib/utils'
import { Body, CloseButton, Footer, Loading, Problem, Select, Win, useErr, useFresh, useMsg, type WinProps } from './parts'

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

/** The sources that need a look, each with Try again. */
export function SourceWarnings() {
  const { t } = useTranslation()
  const msg = useMsg()
  const qc = useQueryClient()
  const name = useSourceName()
  const status = useQuery({ queryKey: Q.crawlStatus, queryFn: S.crawlStatus }).data
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
  return (
    <div className="space-y-2" data-warnings>
      {bad.map((s: SourceHealth) => (
        <div key={s.name} role="alert" className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg border border-warning-border bg-warning-muted px-3 py-2 text-warning-strong">
          <AlertTriangle className="size-5 shrink-0 text-warning" />
          <span className="min-w-48 flex-1" title={s.lastError ? t(s.lastError) : undefined}>
            {t(unhealthyText(s), { source: name(s.name), when: s.lastRun ? msg(ago(s.lastRun)) : t('never') })}
          </span>
          <Button variant="outline" className="h-10" disabled={retry.isPending} onClick={() => retry.mutate(s.name)}>
            <RotateCw />
            {retry.isPending && retry.variables === s.name ? t('Checking…') : t('Try again')}
          </Button>
        </div>
      ))}
    </div>
  )
}

export function SourcesWindow(p: WinProps) {
  const { t } = useTranslation()
  return (
    <Win {...p} title={t('Job sources')} intro={t('We look for jobs on these sites.')}>
      <SourcesBody />
    </Win>
  )
}

function SourcesBody() {
  const { t } = useTranslation()
  const err = useErr()
  const msg = useMsg()
  const qc = useQueryClient()
  const scheds = useFresh(Q.schedule, S.schedule)
  const status = useFresh(Q.crawlStatus, S.crawlStatus)
  const kw = useFresh(Q.keywords, S.keywords)
  const [checking, setChecking] = useState(false)

  const change = useMutation({
    mutationFn: ({ sc, every }: { sc: SourceSchedule; every: number }) => S.setSchedule(sc.source, every),
    // Show the pick at once; put it back if the server says no.
    onMutate: ({ sc, every }) => {
      const before = qc.getQueryData<SourceSchedule[]>(Q.schedule)
      qc.setQueryData<SourceSchedule[]>(Q.schedule, l => l?.map(x => (x.source === sc.source ? { ...x, every_minutes: every, configured: true } : x)))
      return before
    },
    onSuccess: (_, { sc, every }) => toast.success(t('{site}: {schedule}', { site: t(sc.name), schedule: msg(everyLabel(every)) })),
    onError: (e, _, before) => {
      qc.setQueryData(Q.schedule, before)
      toast.error(t("Couldn't change it: {error}", { error: err(e) }))
    },
    onSettled: () => void qc.invalidateQueries({ queryKey: Q.schedule }),
  })

  const failed = scheds.error ?? status.error
  if (failed) return <Body><Problem>{err(failed)}</Problem></Body>
  if (!scheds.ready || !status.ready) return <Body><Loading /></Body>

  const all = scheds.data
  const health = new Map(status.data.sources.map(s => [s.name, s]))
  const sites = all.filter(isSite)
  const others = all.filter(sc => !isSite(sc))
  const on = sites.filter(sc => sc.every_minutes > 0)
  const needTerms = kw.ready && !(kw.data.search_queries ?? []).length

  const check = async () => {
    setChecking(true)
    const runs = await Promise.allSettled(on.map(sc => S.crawl(sc.source)))
    setChecking(false)
    const { added, failed } = checkSummary(runs, on.map(sc => t(sc.name)))
    const n = added.toLocaleString(locale())
    if (failed.length) toast.error(t("Couldn't check {sites}. Found {n} new.", { sites: new Intl.ListFormat(locale(), { type: 'conjunction' }).format(failed), n }))
    else if (added) toast.success(added === 1 ? t('Found 1 new job.') : t('Found {n} new jobs.', { n }))
    else toast(t('No new jobs right now.'))
    for (const k of [Q.crawlStatus, Q.schedule, ['jobs'], Q.activity]) void qc.invalidateQueries({ queryKey: k })
  }

  const row = (sc: SourceSchedule, label: string) => {
    const st = sourceState(sc, health.get(sc.source))
    const unset = !sc.builtin && !sc.configured
    return (
      <div key={sc.source} className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-b py-3 last:border-b-0" data-source={sc.source}>
        <div className="min-w-0">
          <div className="font-semibold">{t(sc.name)}</div>
          <div className={cn('text-small', st.bad ? 'font-medium text-danger' : 'text-muted-foreground')}>
            {t(st.text[0], { time: st.when ? msg(st.when) : '' })}
          </div>
        </div>
        <label className="flex items-center gap-2 text-small">
          <span className="text-muted-foreground">{label}</span>
          <Select
            aria-label={t('{site}: {label}', { site: t(sc.name), label })}
            value={unset ? '' : String(sc.every_minutes)}
            onChange={e => change.mutate({ sc, every: Number(e.target.value) })}
          >
            {unset && (
              <option value="" disabled>
                {t('not set')}
              </option>
            )}
            {intervalOptions(sc).map(o => (
              <option key={o.minutes} value={o.minutes}>
                {msg(o.label)}
              </option>
            ))}
          </Select>
        </label>
      </div>
    )
  }

  return (
    <>
      <Body>
        <SourceWarnings />
        <div>{sites.map(sc => row(sc, t('Check for new jobs:')))}</div>
        {others.length > 0 && (
          <Collapsible>
            <CollapsibleTrigger asChild>
              <Button variant="outline" className="group h-11 w-full justify-between">
                {t('Advanced')}
                <ChevronDown className="transition-transform group-data-[state=open]:rotate-180" />
              </Button>
            </CollapsibleTrigger>
            <CollapsibleContent data-advanced>
              <div className="pt-2">{others.map(sc => row(sc, t('Runs:')))}</div>
            </CollapsibleContent>
          </Collapsible>
        )}
        {needTerms ? (
          <p className="flex flex-wrap items-center gap-x-2" data-hint>
            {t('First tell us what jobs to look for.')}
            <Button variant="link" size="inline" onClick={() => openWindow('keywords')}>
              {t('Add them')}
            </Button>
          </p>
        ) : (
          !on.length && <p data-hint>{t('All sites are off. Turn one on to check it.')}</p>
        )}
      </Body>
      <Footer>
        <CloseButton />
        <Button className="h-11" disabled={checking || needTerms || !on.length || !kw.ready} onClick={() => void check()}>
          <RotateCw className={cn(checking && 'animate-spin')} />
          {checking ? t('Checking…') : t('Check now')}
        </Button>
      </Footer>
    </>
  )
}
