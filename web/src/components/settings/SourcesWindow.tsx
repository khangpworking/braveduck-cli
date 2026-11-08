// Job sources: every job site up front, each with a switch for whether its
// jobs come onto the board (off keeps its login; it just stops bringing
// jobs), how often braveduck checks it, how its last check went, and one
// "Check now". Background tasks and other tools' schedules wait under
// Advanced. A source that needs a look says so at the top.
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ChevronDown, RotateCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Q, S, openWindow, type BoardSource } from '@/lib/settings'
import { checkSummary, everyLabel, intervalOptions, isSite, sourceState, type SourceSchedule } from '@/lib/schedule'
import { locale } from '@/lib/i18n'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'
import { SiteIcon } from '@/components/SiteIcon'
import { Body, CloseButton, Footer, Loading, Problem, Select, Win, useErr, useFresh, useMsg, type WinProps } from './parts'
import { SourceWarnings } from './SourceAlerts'

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
  const board = useFresh(Q.sources, S.sources)
  const [checking, setChecking] = useState(false)

  const turn = useMutation({
    mutationFn: ({ b, on }: { b: BoardSource; on: boolean }) => S.setSource(b.source, on),
    onMutate: ({ b, on }) => {
      const before = qc.getQueryData<BoardSource[]>(Q.sources)
      qc.setQueryData<BoardSource[]>(Q.sources, l => l?.map(x => (x.source === b.source ? { ...x, on } : x)))
      return before
    },
    onSuccess: (_, { b, on }) =>
      toast.success(on ? t('{site}: its jobs come to your board again', { site: t(b.name) }) : t('{site}: off — its jobs no longer come to your board', { site: t(b.name) })),
    onError: (e, _, before) => {
      qc.setQueryData(Q.sources, before)
      toast.error(t("Couldn't change it: {error}", { error: err(e) }))
    },
    onSettled: () => {
      for (const k of [Q.sources, Q.crawlStatus]) void qc.invalidateQueries({ queryKey: k })
    },
  })

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

  const failed = scheds.error ?? status.error ?? board.error
  if (failed) return <Body><Problem>{err(failed)}</Problem></Body>
  if (!scheds.ready || !status.ready || !board.ready) return <Body><Loading /></Body>

  const all = scheds.data
  const health = new Map(status.data.sources.map(s => [s.name, s]))
  const isOn = new Map(board.data.map(b => [b.source, b.on]))
  const others = all.filter(sc => !isSite(sc))
  const on = all.filter(sc => isSite(sc) && sc.every_minutes > 0 && isOn.get(sc.source) !== false)
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

  // A job source: its switch, and how often braveduck checks it when it's a
  // site braveduck checks itself.
  const siteRow = (b: BoardSource) => {
    const sc = all.find(x => x.source === b.source && isSite(x))
    const h = health.get(b.source)
    // Another tool's site (Facebook's scanner) words its state like a site.
    const st = sc || h ? sourceState(sc ?? { builtin: true, task: false }, h) : null
    return (
      <div key={b.source} className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-b py-3 last:border-b-0" data-source={b.source}>
        <label className="flex min-w-0 flex-1 cursor-pointer items-center gap-3">
          <Switch checked={b.on} onCheckedChange={on => turn.mutate({ b, on })} aria-label={t('{site}: bring its jobs to my board', { site: t(b.name) })} />
          <span className="min-w-0">
            <span className="flex items-center gap-2 font-semibold">
              <SiteIcon id={b.source} className="size-5" />
              {t(b.name)}
            </span>
            <span className={cn('block text-small', b.on && st?.bad ? 'font-medium text-danger' : 'text-muted-foreground')}>
              {!b.on ? t('Off — its jobs don’t come to your board') : st ? t(st.text[0], { time: st.when ? msg(st.when) : '' }) : null}
            </span>
          </span>
        </label>
        {sc && b.on && (
          <label className="flex items-center gap-2 text-small">
            <span className="text-muted-foreground">{t('Check for new jobs:')}</span>
            <Select
              aria-label={t('{site}: {label}', { site: t(sc.name), label: t('Check for new jobs:') })}
              value={!sc.builtin && !sc.configured ? '' : String(sc.every_minutes)}
              onChange={e => change.mutate({ sc, every: Number(e.target.value) })}
            >
              {intervalOptions(sc).map(o => (
                <option key={o.minutes} value={o.minutes}>
                  {msg(o.label)}
                </option>
              ))}
            </Select>
          </label>
        )}
      </div>
    )
  }

  const row = (sc: SourceSchedule, label: string) => {
    const st = sourceState(sc, health.get(sc.source))
    const unset = !sc.builtin && !sc.configured
    return (
      <div key={sc.source} className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-b py-3 last:border-b-0" data-source={sc.source}>
        <div className="min-w-0">
          <div className="flex items-center gap-2 font-semibold">
            <SiteIcon id={sc.source} className="size-5" />
            {t(sc.name)}
          </div>
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
        <SourceWarnings here />
        <div>{board.data.map(siteRow)}</div>
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
