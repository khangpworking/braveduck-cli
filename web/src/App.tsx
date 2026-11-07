import { useCallback, useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, RotateCw, Search, Settings as Gear, Sun } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { getJobs, getToday, patchJob, setOnUnauthorized, type Job, type Status } from '@/lib/api'
import { STAGES, matches, sortJobs, stageName, todayEntries } from '@/lib/jobs'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { Toaster } from '@/components/ui/sonner'
import { cn } from '@/lib/utils'
import { N_ } from '@/lib/n'
import { JobRow, TodayCard } from '@/components/board/JobList'
import { JobDetail, type Move } from '@/components/board/JobDetail'
import { AddJob } from '@/components/board/AddJob'
import { Morale } from '@/components/board/Morale'
import { Settings } from '@/components/settings/Settings'
import { GetStarted } from '@/components/settings/GetStarted'

type View = Status | 'today'

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

function NavItem({ active, onClick, label, count }: {
  active?: boolean
  onClick: () => void
  label: React.ReactNode
  count?: number
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-current={active || undefined}
      className={cn(
        'flex h-11 items-center justify-between rounded-lg px-3 text-left text-[1.02rem] hover:bg-muted',
        active && 'bg-primary font-semibold text-primary-foreground hover:bg-primary/90',
      )}
    >
      <span className="flex items-center gap-2">{label}</span>
      {count !== undefined && <span className="tabular-nums opacity-70">{count}</span>}
    </button>
  )
}

export default function App() {
  const { t } = useTranslation()
  const phone = usePhone()
  const qc = useQueryClient()
  const jobsQ = useQuery({ queryKey: ['jobs'], queryFn: getJobs })
  const todayQ = useQuery({ queryKey: ['today'], queryFn: getToday })
  const [view, setView] = useState<View>('saved')
  const [cur, setCur] = useState<number | null>(null)
  const [query, setQuery] = useState('')
  const [adding, setAdding] = useState(false)
  const [settings, setSettings] = useState(false)
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
        toast(t('Keys: / search · n add a job · Esc close a window · ? this list'))
      }
    }
    addEventListener('keydown', onKey)
    return () => removeEventListener('keydown', onKey)
  }, [view, t])

  const jobs = useMemo(() => jobsQ.data ?? [], [jobsQ.data])
  const today = useMemo(() => todayEntries(todayQ.data ?? [], jobs), [todayQ.data, jobs])
  const rows = useMemo(() => {
    if (view === 'today') return today.map(e => e.job)
    return sortJobs(jobs.filter(j => j.status === view && matches(j, query)))
  }, [jobs, view, today, query])
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
  const back = () => (history.state?.job ? history.back() : setPhoneJob(false))

  const save = useMutation({
    mutationFn: ({ id, status }: { id: number; status: Status }) => patchJob(id, { status }),
    onSuccess: () => qc.invalidateQueries(),
    onError: (e: Error) => toast.error(e.message),
  })
  // Go on to the next job in the list, the way a person works through it.
  const goNext = (job: Job) => {
    const i = rows.findIndex(r => r.id === job.id)
    setCur((rows[i + 1] ?? rows[i - 1])?.id ?? null)
    if (phone) back()
  }
  const move: Move = (job, to, opts) => {
    if (opts?.next) goNext(job)
    save.mutate(
      { id: job.id, status: to },
      {
        onSuccess: () =>
          toast(t('Moved to {stage}', { stage: t(stageName(to)) }), {
            action: { label: t('Undo'), onClick: () => save.mutate({ id: job.id, status: job.status }) },
          }),
      },
    )
  }

  const stageList = [
    { id: 'today' as View, name: N_('Today'), n: today.length },
    ...STAGES.map(s => ({ id: s.id as View, name: s.name, n: count(s.id) })),
  ]
  const viewName = t(stageList.find(s => s.id === view)!.name)

  const nav = (
    <nav className="flex w-56 shrink-0 flex-col gap-0.5 border-r bg-muted/40 p-3" aria-label={t('Stages')}>
      <div className="flex items-center gap-2 px-2 pt-1 pb-3 text-lg font-bold">🦆 braveduck</div>
      <Button className="mb-3 h-12 text-base" onClick={() => setAdding(true)}>
        <Plus />
        {t('Add job')}
      </Button>
      <NavItem
        active={view === 'today'}
        onClick={() => setView('today')}
        label={
          <>
            <Sun className="size-4" />
            {t('Today')}
          </>
        }
        count={today.length}
      />
      <div className="px-3 pt-4 pb-1 text-sm text-muted-foreground">{t('Your jobs')}</div>
      {STAGES.map(s => (
        <NavItem key={s.id} active={view === s.id} onClick={() => setView(s.id)} label={t(s.name)} count={count(s.id)} />
      ))}
      <div className="flex-1" />
      <NavItem
        onClick={() => setSettings(true)}
        label={
          <>
            <Gear className="size-4" />
            {t('Settings')}
          </>
        }
      />
    </nav>
  )

  const search = view !== 'today' && (
    <label className="relative block">
      <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
      <Input
        type="search"
        value={query}
        onChange={e => setQuery(e.target.value)}
        placeholder={t('Search jobs')}
        aria-label={t('Search jobs')}
        className="h-11 pl-9 text-base"
      />
    </label>
  )

  const list = (
    <section className="flex min-h-0 min-w-0 flex-1 flex-col md:max-w-[34rem] md:border-r" aria-label={viewName}>
      <div className="space-y-3 px-4 pt-3 pb-2">
        <GetStarted />
        {view !== 'today' && (
          <TodayCard items={today} cap={phone ? 1 : 2} onOpen={j => open(j)} onAll={() => setView('today')} />
        )}
        {phone ? (
          <div className="-mx-4 flex gap-2 overflow-x-auto px-4 pb-1 [scrollbar-width:none]" role="tablist">
            {stageList.map(s => (
              <Button
                key={s.id}
                role="tab"
                aria-selected={view === s.id}
                variant={view === s.id ? 'default' : 'outline'}
                className="h-11 shrink-0 rounded-full px-4 text-base"
                onClick={() => setView(s.id)}
              >
                {t(s.name)} <span className="opacity-70">{s.n}</span>
              </Button>
            ))}
          </div>
        ) : (
          <h2 className="pt-1 text-xl font-semibold">
            {viewName} <span className="font-normal text-muted-foreground">· {t('{n} jobs', { n: rows.length })}</span>
          </h2>
        )}
        {search}
        {(view === 'applied' || view === 'interview' || view === 'offer') && <Morale />}
      </div>
      <Separator />
      <div className="flex-1 overflow-auto">
        {jobsQ.isPending &&
          [0, 1, 2, 3].map(i => (
            <div key={i} className="flex items-center gap-4 border-b px-4 py-3.5">
              <Skeleton className="size-12 rounded-xl" />
              <div className="flex-1 space-y-2">
                <Skeleton className="h-5 w-3/4" />
                <Skeleton className="h-4 w-1/2" />
              </div>
            </div>
          ))}
        {rows.map(j => (
          <JobRow key={j.id} job={j} current={!phone && current?.id === j.id} onOpen={() => open(j, true)} />
        ))}
        {jobsQ.isSuccess && !rows.length && (
          <p className="p-8 text-center text-muted-foreground">
            {query ? t('No job matches “{q}”.', { q: query }) : t('Nothing here yet.')}
          </p>
        )}
        {jobsQ.isError && <p className="p-8 text-center text-red-700">{jobsQ.error.message}</p>}
      </div>
    </section>
  )

  return (
    <>
      {stale && (
        <div role="alert" className="flex items-center justify-center gap-3 bg-amber-100 px-4 py-2 text-amber-900">
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
              <Button className="h-11 px-4 text-base" onClick={() => setAdding(true)}>
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
          {nav}
          {list}
          <JobDetail job={current} phone={false} onBack={() => {}} onMove={move} onDeleted={goNext} />
        </div>
      )}
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
      <Settings
        open={settings}
        onOpenChange={setSettings}
        onOpenJob={id => {
          const j = jobs.find(x => x.id === id)
          if (j) open(j)
        }}
      />
      <Toaster position="bottom-center" />
    </>
  )
}
