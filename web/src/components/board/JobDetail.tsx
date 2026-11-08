import { lazy, memo, useEffect, useState } from 'react'
import { ArrowLeft, ArrowUpRight, Check, ChevronDown, ChevronRight, CircleAlert, Copy, Mail, MapPin, Wallet } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { getJob, getSetup, type Job, type Status } from '@/lib/api'
import { applySite, draftOf, getPlatforms, mailto } from '@/lib/jobApi'
import { GRADES, STAGES, grade, isOpen, km, pay, payUndisclosed, postingUrl, reasons, registerUrl, sourceName, stageName, steps, type Step } from '@/lib/jobs'
import { locale } from '@/lib/i18n'
import { N_ } from '@/lib/n'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuLabel, DropdownMenuRadioGroup, DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { cn } from '@/lib/utils'
import { copy } from '@/lib/clipboard'
import { Deferred, WindowLoading } from '@/components/Deferred'
import { MoreMenu } from '@/components/job/MoreMenu'
import { AboutJob, Documents, Notes, OtherRatings } from '@/components/job/Sections'
import { useJobActions, type JobActions } from '@/components/job/useJobActions'
import { GradeBadge, Warning } from './bits'

const AskAI = lazy(() => import('@/components/job/AskAI').then(m => ({ default: m.AskAI })))
const ApplySteps = lazy(() => import('@/components/job/ApplySteps').then(m => ({ default: m.ApplySteps })))

/** What the board does to a job; App owns the saving and the undo. */
export interface Move {
  (job: Job, to: Status, opts?: { next?: boolean }): void
}

const FOLLOW_UP_DAYS = 7
const DAY = 86_400_000

/** Whole days since an RFC 3339 time, or null. */
const daysSince = (iso: string) => {
  const t = Date.parse(iso)
  return Number.isNaN(t) ? null : Math.floor((Date.now() - t) / DAY)
}

const Kicker = ({ children, tone = 'text-muted-foreground' }: { children: React.ReactNode; tone?: string }) => (
  <span className={cn('text-xs font-bold tracking-wide uppercase', tone)}>{children}</span>
)

function NextStep({ job, a }: { job: Job; a: JobActions }) {
  const { t } = useTranslation()
  const c = job.clock
  const keepHint = job.keep ? '' : ' ' + t('To stop that, choose Keep in the ⋯ menu.')
  if (job.status === 'applied' && c?.kind === 'applied') {
    const asked = daysSince(job.followup_sent_at)
    const quiet = asked ?? c.days
    if (c.state !== 'silent' && !job.keep && !job.pinned) {
      // The sweep's countdown: no answer for weeks closes the application.
      return (
        <Card size="callout" tone="danger">
          <Kicker tone="text-danger">{t('No answer yet')}</Kicker>
          <span className="text-lg font-semibold">
            {t('Moves to Closed in {n} days', { n: Math.max(0, c.closes_in ?? 0) })}
          </span>
          <span className="text-muted-foreground">
            {t('Applications with no answer for {n} days move to Closed.', { n: 27 })}
            {keepHint}
          </span>
        </Card>
      )
    }
    if (quiet >= FOLLOW_UP_DAYS) {
      const sent = new Date(c.date).toLocaleDateString(locale(), { day: 'numeric', month: 'numeric' })
      const msg = t('Hello, I applied for the {title} position on {date}. I would be glad to talk more about it.', { title: job.title, date: sent })
      return (
        <Card size="callout" tone="warning">
          <Kicker tone="text-warning">{t('Next step')}</Kicker>
          <span className="text-xl font-semibold">{t('Ask the company for an answer')}</span>
          <span className="text-muted-foreground">
            {asked === null
              ? t('No answer for {n} days. A short message is enough:', { n: c.days })
              : t('Still no answer {n} days after you asked. Ask once more:', { n: asked })}
          </span>
          <div className="rounded-lg border bg-white px-3.5 py-2.5">{msg}</div>
          <div className="grid gap-2 sm:grid-cols-2">
            <Button variant="outline" className="h-11" onClick={() => void copy(msg, t)}>
              <Copy />
              {t('Copy message')}
            </Button>
            <Button variant="outline" className="h-11" disabled={a.followedUp.isPending} onClick={() => a.followedUp.mutate()}>
              <Check />
              {t('I’ve sent it')}
            </Button>
          </div>
        </Card>
      )
    }
    return (
      <Card size="callout">
        <Kicker>{t('Next step')}</Kicker>
        <span className="text-lg font-semibold">{t('Wait for the company to answer')}</span>
        <span className="text-muted-foreground">
          {asked !== null
            ? t('You asked {d} days ago. If there is no answer in {n} days, braveduck reminds you again.', { d: asked, n: FOLLOW_UP_DAYS })
            : c.days === 0
              ? t('Applied today. If there is no answer in {n} days, braveduck will remind you to ask.', { n: FOLLOW_UP_DAYS })
              : t('Applied {d} days ago. If there is no answer in {n} days, braveduck will remind you to ask.', { d: c.days, n: FOLLOW_UP_DAYS })}
        </span>
      </Card>
    )
  }
  if (isOpen(job) && c?.kind === 'deadline' && c.days <= 7) {
    const d = c.days
    const gone = d < 0
    return (
      <Card size="callout" tone={gone ? 'danger' : 'warning'}>
        <span className={cn('text-xl font-semibold', gone && 'text-danger')}>
          {gone
            ? t('This posting has closed')
            : d === 0
              ? t('Closes today')
              : d === 1
                ? t('Closes tomorrow')
                : t('{n} days left to apply', { n: d })}
        </span>
        <span className="text-muted-foreground">
          {!gone
            ? t('Apply early so you don’t miss it.')
            : job.keep || job.pinned
              ? t('You kept it, so it stays here.')
              : t('It moves to Closed in {n} days.', { n: Math.max(0, c.closes_in ?? 0) }) + keepHint}
        </span>
      </Card>
    )
  }
  return null
}

const TOPCV: Record<string, string> = {
  sent: N_('TopCV: CV sent'),
  seen: N_('TopCV: the company saw your CV'),
  suitable: N_('TopCV: the company finds you suitable'),
  rejected: N_('TopCV: not suitable'),
}

function Reasons({ job, a }: { job: Job; a: JobActions }) {
  const { t } = useTranslation()
  const list = reasons(job)
  const breakers = job.dealbreakers ?? []
  return (
    <div className="space-y-4">
      {breakers.length > 0 && (
        <div>
          <h3 className="mb-1 text-sm font-semibold text-muted-foreground">{t('Know before you apply')}</h3>
          <Warning job={job} all />
        </div>
      )}
      {list.length > 0 && (
        <div>
          <h3 className="mb-1 text-sm font-semibold text-muted-foreground">{t('Why this grade')}</h3>
          <ul className="divide-y">
            {list.map(w => (
              <li key={w} className="flex items-start gap-2.5 py-2.5 text-body">
                <ChevronRight className="mt-1 size-4 shrink-0 text-muted-foreground" />
                {w}
              </li>
            ))}
          </ul>
        </div>
      )}
      <Button
        variant="quiet"
        className="h-9"
        disabled={a.askAI.isPending || job.score_status === 'pending'}
        onClick={() => a.askAI.mutate({ kind: 'rate' })}
      >
        {job.score_status === 'scored' ? t('Rate it again') : t('Rate it now')}
      </Button>
    </div>
  )
}

/** What the business register says about the company, and its page there. */
function Registered({ job }: { job: Job }) {
  const { t } = useTranslation()
  const link = registerUrl(job)
  if (!link) return null
  const d = job.company_active_date
  const day = /^\d{4}-\d{2}-\d{2}$/.test(d) ? new Date(`${d}T00:00:00`) : null
  const found = d !== 'not_found'
  return (
    <p className="text-small text-muted-foreground">
      {!found
        ? t('Not in the business register by this name')
        : day
          ? t('In business since {date}', { date: day.toLocaleDateString(locale(), { day: 'numeric', month: 'numeric', year: 'numeric' }) })
          : t('In the business register')}
      {found && job.company_tax_code && <> · {t('Tax code {code}', { code: job.company_tax_code })}</>}
      {' · '}
      <a href={link} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-0.5 underline underline-offset-2">
        {found ? t('See it on dulieuphapluat.vn') : t('Look it up on dulieuphapluat.vn')}
        <ArrowUpRight className="size-3.5" />
      </a>
    </p>
  )
}

function Facts({ job }: { job: Job }) {
  const { t } = useTranslation()
  const k = km(job)
  const where =
    k !== null
      ? t('{n} km from home', { n: k })
      : job.company_geo_status === 'no_address'
        ? t('No address')
        : job.company_geo_status === 'geocode_failed'
          ? t('Address not found on the map')
          : '–'
  return (
    <div className="grid grid-cols-2 gap-2.5">
      <Card size="fact">
        <span className="flex items-center gap-1.5 text-sm text-muted-foreground">
          <Wallet className="size-4" />
          {t('Pay')}
        </span>
        <b className="text-body">{payUndisclosed(job.salary) ? t('Pay not given') : pay(job.salary, locale())}</b>
      </Card>
      <Card size="fact">
        <span className="flex items-center gap-1.5 truncate text-sm text-muted-foreground">
          <MapPin className="size-4 shrink-0" />
          <span className="truncate">{job.location || t('Place not given')}</span>
        </span>
        <b className="text-body">{where}</b>
      </Card>
    </div>
  )
}

/**
 * A part of the job's page, with its name: what's near belongs together, and
 * the space between the parts says where one ends.
 */
function Group({ title, first = false, children }: { title: string; first?: boolean; children: React.ReactNode }) {
  return (
    <section className={cn('space-y-3', !first && 'pt-6')} aria-label={title}>
      <h2 className="text-xs font-bold tracking-wide text-muted-foreground uppercase">{title}</h2>
      {children}
    </section>
  )
}

const STEP_NAMES: Record<Step['stage'], string> = {
  saved: N_('Saved'),
  waiting: N_('Ready to apply'),
  applied: N_('Applied'),
  followup: N_('Asked the company'),
  interview: N_('Interview'),
  offer: N_('Offer'),
  closed: N_('Closed'),
}

/** The steps so far, joined by a line, with the dates braveduck knows. */
function Timeline({ job }: { job: Job }) {
  const { t } = useTranslation()
  const list = steps(job)
  return (
    <ol>
      {list.map((s, i) => {
        const last = i === list.length - 1
        return (
          <li key={s.stage} className="flex gap-3">
            <span className="flex flex-col items-center">
              <span className={cn('mt-1 size-4 shrink-0 rounded-full', last ? 'bg-primary ring-4 ring-primary/15' : 'bg-muted-foreground/60')} />
              {!last && <span className="w-0.5 flex-1 bg-border" />}
            </span>
            <span className={cn('pb-4', last && 'pb-0')}>
              <span className={cn('block', last && 'font-semibold')}>{t(STEP_NAMES[s.stage])}</span>
              {s.date && (
                <span className="block text-sm text-muted-foreground">
                  {new Date(s.date).toLocaleDateString(locale(), { day: 'numeric', month: 'long' })}
                </span>
              )}
            </span>
          </li>
        )
      })}
    </ol>
  )
}

function GradeCard({ job }: { job: Job }) {
  const { t } = useTranslation()
  const g = grade(job)
  const pending = job.score_status === 'pending'
  return (
    <div className="flex items-center gap-4 rounded-xl bg-muted/70 p-4">
      <GradeBadge job={job} size="lg" />
      <div>
        <div className="text-xl font-semibold">{pending ? t('Rating…') : t(GRADES[g].label)}</div>
        {g === '?' && !pending && (
          <div className="text-muted-foreground">{t('Fill in your profile and braveduck rates jobs for you.')}</div>
        )}
      </div>
    </div>
  )
}

function MoveMenu({ job, onMove }: { job: Job; onMove: Move }) {
  const { t } = useTranslation()
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="quiet" className="h-10">
          {t('Move to…')}
          <ChevronDown />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel>{t('Move this job to')}</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={job.status} onValueChange={v => v !== job.status && onMove(job, v as Status)}>
          {STAGES.map(s => (
            <DropdownMenuRadioItem key={s.id} value={s.id} className="h-10">
              {t(s.name)}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/** The one main action for the job's stage. */
function MainAction({ job, a, onMove }: { job: Job; a: JobActions; onMove: Move }) {
  const { t } = useTranslation()
  const [asking, setAsking] = useState(false)
  useEffect(() => setAsking(false), [job.id])
  const platforms = useQuery({ queryKey: ['platforms'], queryFn: getPlatforms })
  const setup = useQuery({ queryKey: ['setup'], queryFn: getSetup })
  const url = postingUrl(job)
  const site = url ? applySite(job, platforms.data ?? []) : undefined

  if (job.apply_state === 'confirm') {
    return (
      <div className="space-y-2 text-center">
        <p className="text-lg font-semibold">
          {site ? t('Did you submit it on {site}?', { site: site.name }) : t('Did you submit it?')}
        </p>
        <div className="flex gap-2">
          <Button size="xl" className="flex-1" onClick={() => a.confirm.mutate(true)}>
            {t('Yes, I applied')}
          </Button>
          <Button variant="outline" size="xl" className="flex-1" onClick={() => a.confirm.mutate(false)}>
            {t('Not yet')}
          </Button>
        </div>
      </div>
    )
  }
  if (isOpen(job)) {
    if (job.apply_state === 'filling') {
      return (
        <Button size="xl" className="w-full" disabled>
          {t('Applying in the {site} window…', { site: site?.name ?? '' })}
        </Button>
      )
    }
    if (asking || !url) {
      return (
        <div className="space-y-2 text-center">
          <p className="text-lg font-semibold">{t('Did you send your CV?')}</p>
          <div className="flex gap-2">
            <Button size="xl" className="flex-1" onClick={() => onMove(job, 'applied', { next: true })}>
              {t('Yes, I applied')}
            </Button>
            {url && (
              <Button variant="outline" size="xl" className="flex-1" onClick={() => setAsking(false)}>
                {t('Not yet')}
              </Button>
            )}
          </div>
        </div>
      )
    }
    const canAssist = site && !job.topcv_status && (job.cv_status === 'ready' || setup.data?.cv)
    if (canAssist) {
      return (
        <Button size="xl" className="w-full" disabled={a.apply.isPending} onClick={() => a.apply.mutate()}>
          {t('Apply on {site}', { site: site.name })}
        </Button>
      )
    }
    const d = draftOf(job)
    if (d && job.draft_status !== 'sent' && job.apply_method !== 'platform') {
      return (
        <Button asChild size="xl" className="w-full">
          <a href={mailto(d)} onClick={() => setAsking(true)}>
            <Mail />
            {t('Send from my mail app')}
          </a>
        </Button>
      )
    }
    return (
      <Button asChild size="xl" className="w-full">
        <a href={url} target="_blank" rel="noopener noreferrer" onClick={() => setAsking(true)}>
          {t('Open the posting to apply')}
          <ArrowUpRight />
        </a>
      </Button>
    )
  }
  const next: Partial<Record<Status, [Status, string]>> = {
    applied: ['interview', N_('The company called me for an interview')],
    interview: ['offer', N_('I got an offer')],
    offer: ['closed', N_('Close this job')],
    closed: ['saved', N_('Put it back in Saved')],
  }
  const n = next[job.status]
  if (!n) return null
  return (
    <Button size="xl" className="w-full" onClick={() => onMove(job, n[0])}>
      {t(n[1])}
    </Button>
  )
}

export const JobDetail = memo(function JobDetail({ job, phone, onBack, onMove, onDeleted }: {
  job: Job | null
  phone: boolean
  onBack: () => void
  onMove: Move
  onDeleted: (j: Job) => void
}) {
  const { t } = useTranslation()
  if (!job) {
    return (
      <div className="flex flex-1 items-center justify-center p-8 text-center text-muted-foreground">
        {t('Nothing here yet. Press “Add job” to add one.')}
      </div>
    )
  }
  return <WithText key={job.id} job={job} phone={phone} onBack={onBack} onMove={onMove} onDeleted={onDeleted} />
})

type DetailProps = {
  job: Job
  phone: boolean
  onBack: () => void
  onMove: Move
  onDeleted: (j: Job) => void
}

/**
 * The list's copy of a job leaves out its long text (the posting, its AI
 * summary, the email draft); the page fetches the job once to fill it in,
 * and keeps everything else from the list, which is never staler.
 */
function WithText(p: DetailProps) {
  const full = useQuery({ queryKey: ['job', p.job.id], queryFn: () => getJob(p.job.id) }).data
  const job = full
    ? { ...p.job, jd_full: full.jd_full, jd_ai_summary: full.jd_ai_summary, draft_email: full.draft_email }
    : p.job
  return <Detail {...p} job={job} />
}

function Detail({ job, phone, onBack, onMove, onDeleted }: {
  job: Job
  phone: boolean
  onBack: () => void
  onMove: Move
  onDeleted: (j: Job) => void
}) {
  const { t } = useTranslation()
  const a = useJobActions(job)
  const early = isOpen(job)
  const found = new Date(job.saved_at || job.created_at)
  const status = [
    TOPCV[job.topcv_status] ? t(TOPCV[job.topcv_status]) : '',
    job.apply_state === 'sent' ? t('Application sent') : '',
  ].filter(Boolean)
  return (
    <section
      aria-label={job.title}
      className={cn('flex min-h-0 min-w-0 flex-1 flex-col bg-background', phone && 'min-h-dvh')}
    >
      {phone && (
        <button
          type="button"
          onClick={onBack}
          className="sticky top-0 z-10 flex h-14 items-center gap-2 border-b bg-background px-4 text-lg font-semibold"
        >
          <ArrowLeft className="size-5" />
          {t('Back to the list')}
        </button>
      )}
      <div className="flex-1 space-y-4 overflow-auto px-5 py-6 md:px-8">
        <div className="flex items-start gap-2">
          <div className="min-w-0 flex-1">
            <h1 className="text-title leading-tight font-semibold tracking-tight">{job.title}</h1>
            <p className="text-body text-muted-foreground">{job.company}</p>
            <Registered job={job} />
          </div>
          <MoreMenu job={job} a={a} onDeleted={() => onDeleted(job)} />
        </div>
        <NextStep job={job} a={a} />
        {status.length > 0 && <p className="font-medium text-success">{status.join(' · ')}</p>}
        <Group title={t('How it fits you')} first>
        {early ? (
          <>
            <GradeCard job={job} />
            <Facts job={job} />
            <Reasons job={job} a={a} />
          </>
        ) : (
          <>
            <Facts job={job} />
            <Collapsible>
              <CollapsibleTrigger asChild>
                <button type="button" className="group flex min-h-12 w-full items-center gap-3 text-left font-semibold">
                  <GradeBadge job={job} />
                  {grade(job) === '?' ? t('Not rated yet') : t('{label}: see why', { label: t(GRADES[grade(job)].label) })}
                  <ChevronRight className="ml-auto size-5 transition-transform group-data-[state=open]:rotate-90" />
                </button>
              </CollapsibleTrigger>
              <CollapsibleContent>
                <div className="pt-3">
                  <Reasons job={job} a={a} />
                </div>
              </CollapsibleContent>
            </Collapsible>
          </>
        )}
        {job.score_status === 'failed' && (
          <p className="flex items-center gap-2 text-warning-strong">
            <CircleAlert className="size-4" />
            {t("Couldn't rate this job. braveduck tries again later.")}
          </p>
        )}
        </Group>
        <Group title={t('Your part')}>
          <Notes job={job} a={a} />
          {(early || job.status === 'applied') && <Documents job={job} a={a} />}
        </Group>
        {job.status !== 'saved' && (
          <Group title={t('So far')}>
            <Timeline job={job} />
          </Group>
        )}
        <Group title={t('About the posting')}>
          <AboutJob job={job} />
          <OtherRatings job={job} a={a} />
        </Group>
        <p className="text-sm text-muted-foreground">
          {t('Found on {source}, {date}', {
            source: job.discovery_source ? t(sourceName(job.discovery_source)) : t('your list'),
            date: found.toLocaleDateString(locale(), { day: 'numeric', month: 'long' }),
          })}
          {' · '}
          {t(stageName(job.status))}
          {postingUrl(job) && (
            <>
              {' · '}
              <a href={postingUrl(job)} target="_blank" rel="noopener noreferrer" className="underline underline-offset-2">
                {t('See the posting')}
              </a>
            </>
          )}
        </p>
      </div>
      <div className={cn('space-y-1 border-t bg-background px-5 py-3 md:px-8', phone && 'sticky bottom-0')}>
        <MainAction job={job} a={a} onMove={onMove} />
        <div className="flex items-center justify-between">
          {job.status === 'saved' && job.apply_state !== 'confirm' ? (
            <Button variant="quiet" className="h-10" onClick={() => onMove(job, 'closed', { next: true })}>
              {t('Not a fit, skip it')}
            </Button>
          ) : (
            <span />
          )}
          <MoveMenu job={job} onMove={onMove} />
        </div>
      </div>
      {a.ask && (
        <Deferred active fallback={<WindowLoading title={t(a.ask.kind === 'rate' ? 'Rate this job with your AI' : a.ask.kind === 'cv' ? 'Tailor your CV with your AI' : 'Draft the email with your AI')} className="sm:max-w-xl" onClose={() => a.setAsk(null)} />}>
          <AskAI job={job} kind={a.ask.kind} ask={a.ask.ask} onClose={() => a.setAsk(null)} />
        </Deferred>
      )}
      <Deferred active={!!a.prep} fallback={<WindowLoading title={t('A {site} window is opening', { site: a.prep?.site ?? '' })} onClose={() => a.setPrep(null)} />}>
        <ApplySteps a={a} />
      </Deferred>
    </section>
  )
}
