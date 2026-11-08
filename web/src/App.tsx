import { lazy, useCallback, useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, RotateCw, Search, Settings as Gear, Zap } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { getJobs, getToday, jobChanged, patchJob, setOnUnauthorized, type Job, type Status } from '@/lib/api'
import { STAGES, matches, sortJobs, stageName, todayEntries } from '@/lib/jobs'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { Toaster } from '@/components/ui/sonner'
import { N_ } from '@/lib/n'
import { JobRow, TodayCard } from '@/components/board/JobList'
import { Deferred, WindowLoading } from '@/components/Deferred'
import { SortMenu, useSort } from '@/components/board/SortMenu'
import { Nav, NavRail, useMenuOpen, type View } from '@/components/board/Nav'
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from '@/components/ui/resizable'
import { useDefaultLayout, useGroupRef } from 'react-resizable-panels'
import { JobDetail, type Move } from '@/components/board/JobDetail'
import { Morale } from '@/components/board/Morale'
import { Settings } from '@/components/settings/Settings'
import { GetStarted } from '@/components/settings/GetStarted'
import { SourceWarnings } from '@/components/settings/SourceAlerts'

const QuickReview = lazy(() => import('@/components/board/QuickReview').then(m => ({ default: m.QuickReview })))
const AddJob = lazy(() => import('@/components/board/AddJob').then(m => ({ default: m.AddJob })))

const PHONE = '(max-width: 760px)'

function usePhone() {
  const [phone, setPhone] = useState(() => window.matchMedia(PHONE).matches)
  useEffect(() => {
    const m = window.matchMedia(PHONE)
    const on = () => setPhone(m.matches)
    m.addEventListener('change', on)
    return () => m.removeEventListener('change', on)
  }, [])
  return phone
}


export default function App() {
  const { t } = useTranslation()
  const phone = usePhone()
  const qc = useQueryClient()
  const jobsQ = useQuery({ queryKey: ['jobs'], queryFn: getJobs })
  const todayQ = useQuery({ queryKey: ['today'], queryFn: getToday })
  const [view, setView] = useState<View>('saved')
  const [sort, setSort] = useSort(view)
  const [cur, setCur] = useState<number | null>(null)
  const [query, setQuery] = useState('')
  const [reviewing, setReviewing] = useState(false)
  const [adding, setAdding] = useState(false)
  const [settings, setSettings] = useState(false)
  const [menuOpen, setMenuOpen] = useMenuOpen()
  const layout = useDefaultLayout({ id: 'braveduck-board', storage: localStorage })
  const panels = useGroupRef()
  const [phoneJob, setPhoneJob] = useState(false)
  const [stale, setStale] = useState(false)
  useEffect(() => setOnUnauthorized(() => setStale(true)), [])

  // Keys: / searches, n adds a job, ? lists the keys. Not while typing, and
  // not with Ctrl, Alt or Cmd (those belong to the browser).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement
      if (e.ctrlKey || e.metaKey || e.altKey || el.closest('input, textarea, select, [contenteditable=true], [role=dialog]')) return
      if (e.key === '/') {
        e.preventDefault()
        if (view === 'today') setView('saved')
        setTimeout(() => document.querySelector<HTMLInputElement>('input[type=search]')?.focus())
      } else if (e.key === 'n') {
        e.preventDefault()
        setAdding(true)
      } else if (e.key === '?') {
        toast(t('Keys: / search · n add a job · [ fold the menu · Esc close a window · ? this list'))
      }
    }
    addEventListener('keydown', onKey)
    return () => removeEventListener('keydown', onKey)
  }, [view, t])

  const jobs = useMemo(() => jobsQ.data ?? [], [jobsQ.data])
  const today = useMemo(() => todayEntries(todayQ.data ?? [], jobs), [todayQ.data, jobs])
  const rows = useMemo(() => {
    if (view === 'today') return today.map(e => e.job)
    return sortJobs(jobs.filter(j => j.status === view && matches(j, query)), sort)
  }, [jobs, view, today, query, sort])
  const current = rows.find(j => j.id === cur) ?? (phone ? null : (rows[0] ?? null))
  const count = (s: Status) => jobs.filter(j => j.status === s).length

  // On a phone the detail is its own screen, so the phone's Back button
  // returns to the list instead of leaving the board.
  useEffect(() => {
    const onPop = () => setPhoneJob(false)
    addEventListener('popstate', onPop)
    return () => removeEventListener('popstate', onPop)
  }, [])

  const open = useCallback(
    (j: Job, stay = false) => {
      if (!stay) setView(j.status)
      setCur(j.id)
      if (phone) {
        setPhoneJob(true)
        history.pushState({ job: j.id }, '')
        scrollTo(0, 0)
      }
    },
    [phone],
  )
  const openRow = useCallback((job: Job) => open(job, true), [open])
  const showToday = useCallback(() => setView('today'), [])
  const back = useCallback(() => (history.state?.job ? history.back() : setPhoneJob(false)), [])

  const save = useMutation({
    mutationFn: ({ id, status }: { id: number; status: Status }) => patchJob(id, { status }),
    onSuccess: (_, v) => jobChanged(qc, v.id),
    onError: (e: Error) => toast.error(e.message),
  })
  // Go on to the next job in the list, the way a person works through it.
  const goNext = useCallback((job: Job) => {
    const i = rows.findIndex(r => r.id === job.id)
    setCur((rows[i + 1] ?? rows[i - 1])?.id ?? null)
    if (phone) back()
  }, [rows, phone, back])
  const mutate = save.mutate
  const move: Move = useCallback((job, to, opts) => {
    if (opts?.next) goNext(job)
    mutate(
      { id: job.id, status: to },
      {
        onSuccess: () =>
          toast(t('Moved to {stage}', { stage: t(stageName(to)) }), {
            action: { label: t('Undo'), onClick: () => mutate({ id: job.id, status: job.status }) },
          }),
      },
    )
  }, [goNext, mutate, t])

  const stageList = [
    { id: 'today' as View, name: N_('Today'), n: today.length },
    ...STAGES.map(s => ({ id: s.id as View, name: s.name, n: count(s.id) })),
  ]
  const viewName = t(stageList.find(s => s.id === view)!.name)

  const counts = { today: today.length, ...Object.fromEntries(STAGES.map(st => [st.id, count(st.id)])) } as Record<View, number>
  const navProps = { view, setView, counts, onAdd: () => setAdding(true), onSettings: () => setSettings(true), onFold: () => setMenuOpen(o => !o) }

  const search = view !== 'today' && (
    <label className="relative block">
      <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
      <Input
        type="search"
        value={query}
        onChange={e => setQuery(e.target.value)}
        placeholder={t('Search jobs')}
        aria-label={t('Search jobs')}
        className="pl-9"
      />
    </label>
  )

  const list = (
    <section className="flex h-full min-h-0 min-w-0 flex-1 flex-col" aria-label={viewName}>
      <div className="space-y-3 px-4 pt-3 pb-2">
        <GetStarted />
        <SourceWarnings />
        {view !== 'today' && (
          <TodayCard items={today} cap={phone ? 1 : 2} onOpen={open} onAll={showToday} />
        )}
        {phone ? (
          <div className="-mx-4 flex gap-2 overflow-x-auto px-4 pb-1 no-scrollbar" role="tablist">
            {stageList.map(s => (
              <Button
                key={s.id}
                role="tab"
                aria-selected={view === s.id}
                variant={view === s.id ? 'default' : 'outline'}
                size="chip"
                className="shrink-0"
                onClick={() => setView(s.id)}
              >
                {t(s.name)} <span className="opacity-70">{s.n}</span>
              </Button>
            ))}
          </div>
        ) : (
          <div className="flex items-center gap-2 pt-1">
            <h2 className="flex-1 text-xl font-semibold">
              {viewName} <span className="font-normal text-muted-foreground">· {t('{n} jobs', { n: rows.length })}</span>
            </h2>
            {view !== 'today' && <SortMenu sort={sort} onSort={setSort} />}
          </div>
        )}
        {search}
        {(view === 'saved' || view === 'waiting') && rows.length > 1 && (
          <Button variant="outline" className="w-full" onClick={() => setReviewing(true)}>
            <Zap />
            {t('Go through them one by one ({n})', { n: rows.length })}
          </Button>
        )}
        {(view === 'applied' || view === 'interview' || view === 'offer') && <Morale />}
      </div>
      <Separator />
      <div className="flex-1 overflow-auto">
        {jobsQ.isPending &&
          [0, 1, 2, 3].map(i => (
            <div key={i} className="flex items-center gap-4 border-b px-4 py-3.5">
              <Skeleton className="size-12" />
              <div className="flex-1 space-y-2">
                <Skeleton className="h-5 w-3/4" />
                <Skeleton className="h-4 w-1/2" />
              </div>
            </div>
          ))}
        {rows.map(j => (
          <JobRow key={j.id} job={j} current={!phone && current?.id === j.id} onOpen={openRow} />
        ))}
        {jobsQ.isSuccess && !rows.length && (
          <p className="p-8 text-center text-muted-foreground">
            {query ? t('No job matches “{q}”.', { q: query }) : t('Nothing here yet.')}
          </p>
        )}
        {jobsQ.isError && <p className="p-8 text-center text-danger">{jobsQ.error.message}</p>}
      </div>
    </section>
  )

  return (
    <>
      {stale && (
        <div role="alert" className="flex items-center justify-center gap-3 bg-warning-border/40 px-4 py-2 text-warning-strong">
          {t('braveduck restarted. Reload the page to carry on.')}
          <Button size="sm" variant="outline" onClick={() => location.reload()}>
            <RotateCw />
            {t('Reload')}
          </Button>
        </div>
      )}
      {phone ? (
        phoneJob && current ? (
          <JobDetail job={current} phone onBack={back} onMove={move} onDeleted={goNext} />
        ) : (
          <div className="min-h-dvh">
            <header className="sticky top-0 z-10 flex items-center gap-2 border-b bg-background px-4 py-2.5">
              <span className="flex-1 text-lg font-bold">🦆 braveduck</span>
              <Button onClick={() => setAdding(true)}>
                <Plus />
                {t('Add job')}
              </Button>
              <Button variant="outline" size="icon" className="size-11" aria-label={t('Settings')} onClick={() => setSettings(true)}>
                <Gear className="size-5" />
              </Button>
            </header>
            {list}
          </div>
        )
      ) : (
        <div className="flex h-dvh">
          {menuOpen ? <Nav {...navProps} /> : <NavRail {...navProps} />}
          {/* The list and the job share the rest; drag the bar between them
              (remembered), double-click it to even them out again. */}
          <ResizablePanelGroup
            id="braveduck-board"
            defaultLayout={layout.defaultLayout}
            onLayoutChanged={layout.onLayoutChanged}
            groupRef={panels}
          >
            <ResizablePanel id="list" defaultSize="40" minSize={320}>
              {list}
            </ResizablePanel>
            <ResizableHandle
              withHandle
              aria-label={t('Drag to make the list or the job wider')}
              title={t('Drag to resize · double-click to reset')}
              onDoubleClick={() => panels.current?.setLayout({ list: 40, detail: 60 })}
            />
            <ResizablePanel id="detail" minSize={400}>
              <div className="flex h-full">
                <JobDetail job={current} phone={false} onBack={back} onMove={move} onDeleted={goNext} />
              </div>
            </ResizablePanel>
          </ResizablePanelGroup>
        </div>
      )}
      <Deferred active={adding} fallback={<WindowLoading title={t('Add a job')} onClose={() => setAdding(false)} />}>
        <AddJob
          open={adding}
          onOpenChange={setAdding}
          onAdded={id => {
            // The new job arrives with the refetch; show it once it's there.
            setCur(id)
            void qc.refetchQueries({ queryKey: ['jobs'] }).then(() => {
              const j = qc.getQueryData<Job[]>(['jobs'])?.find(x => x.id === id)
              if (j) open(j)
            })
          }}
        />
      </Deferred>
      <Settings
        open={settings}
        onOpenChange={setSettings}
        onOpenJob={id => {
          const j = jobs.find(x => x.id === id)
          if (j) open(j)
        }}
      />
      <Deferred active={reviewing} fallback={<WindowLoading title={t('Go through your jobs')} className="sm:max-w-2xl" onClose={() => setReviewing(false)} />}>
        <QuickReview
          jobs={rows}
          open={reviewing}
          onOpenChange={setReviewing}
          onMove={(job, to) => save.mutate({ id: job.id, status: to })}
        />
      </Deferred>
      <Toaster position="bottom-center" />
    </>
  )
}
