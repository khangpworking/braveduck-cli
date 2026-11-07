import { useEffect, useState } from 'react'
import {
  ArrowLeft, ArrowUpRight, ChevronDown, ChevronRight, CircleAlert, Copy, MapPin, Wallet,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import type { Job, Status } from '@/lib/api'
import { GRADES, STAGES, grade, isOpen, km, pay, payUndisclosed, reasons, sourceName, stageName } from '@/lib/jobs'
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
import { GradeBadge, Warning } from './bits'

/** What the board does to a job; App owns the saving and the undo. */
export interface Move {
  (job: Job, to: Status, opts?: { next?: boolean }): void
}

const FOLLOW_UP_DAYS = 7

function NextStep({ job }: { job: Job }) {
  const { t } = useTranslation()
  const c = job.clock
  if (job.status === 'applied' && c?.kind === 'applied') {
    if (c.days >= FOLLOW_UP_DAYS) {
      const sent = new Date(c.date).toLocaleDateString(locale(), { day: 'numeric', month: 'numeric' })
      const msg = t('Hello, I applied for the {title} position on {date}. I would be glad to talk more about it.', { title: job.title, date: sent })
      return (
        <Card className="gap-2 border-amber-300 bg-amber-50 px-5 py-4">
          <span className="text-xs font-bold tracking-wide text-amber-700 uppercase">{t('Next step')}</span>
          <span className="text-xl font-semibold">{t('Ask the company for an answer')}</span>
          <span className="text-muted-foreground">{t('No answer for {n} days. A short message is enough:', { n: c.days })}</span>
          <div className="rounded-lg border bg-white px-3.5 py-2.5">{msg}</div>
          <div>
            <Button
              variant="outline"
              className="h-11 px-4 text-base"
              onClick={() =>
                navigator.clipboard.writeText(msg).then(
                  () => toast(t('Copied. Paste it into an email or Zalo to the company.')),
                  () => toast.error(t("Couldn't copy. Select the text and copy it yourself.")),
                )
              }
            >
              <Copy />
              {t('Copy message')}
            </Button>
          </div>
        </Card>
      )
    }
    return (
      <Card className="gap-1 px-5 py-4">
        <span className="text-xs font-bold tracking-wide text-muted-foreground uppercase">{t('Next step')}</span>
        <span className="text-lg font-semibold">{t('Wait for the company to answer')}</span>
        <span className="text-muted-foreground">
          {c.days === 0
            ? t('Applied today. If there is no answer in {n} days, braveduck will remind you to ask.', { n: FOLLOW_UP_DAYS })
            : t('Applied {d} days ago. If there is no answer in {n} days, braveduck will remind you to ask.', { d: c.days, n: FOLLOW_UP_DAYS })}
        </span>
      </Card>
    )
  }
  if (isOpen(job) && c?.kind === 'deadline' && c.days <= 7) {
    const d = c.days
    return (
      <Card className={cn('gap-1 px-5 py-4', d < 0 ? 'border-red-200 bg-red-50' : 'border-amber-300 bg-amber-50')}>
        <span className={cn('text-xl font-semibold', d < 0 && 'text-red-800')}>
          {d < 0
            ? t('This posting has closed')
            : d === 0
              ? t('Closes today')
              : d === 1
                ? t('Closes tomorrow')
                : t('{n} days left to apply', { n: d })}
        </span>
        <span className="text-muted-foreground">{d < 0 ? t('It moves to Closed by itself soon.') : t('Apply early so you don’t miss it.')}</span>
      </Card>
    )
  }
  return null
}

function Reasons({ job }: { job: Job }) {
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
              <li key={w} className="flex items-start gap-2.5 py-2.5 text-[1.02rem]">
                <ChevronRight className="mt-1 size-4 shrink-0 text-muted-foreground" />
                {w}
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  )
}

function Facts({ job }: { job: Job }) {
  const { t } = useTranslation()
  const k = km(job)
  return (
    <div className="grid grid-cols-2 gap-2.5">
      <Card className="gap-0.5 px-4 py-3">
        <span className="flex items-center gap-1.5 text-sm text-muted-foreground">
          <Wallet className="size-4" />
          {t('Pay')}
        </span>
        <b className="text-[1.06rem]">{payUndisclosed(job.salary) ? t('Pay not given') : pay(job.salary, locale())}</b>
      </Card>
      <Card className="gap-0.5 px-4 py-3">
        <span className="flex items-center gap-1.5 truncate text-sm text-muted-foreground">
          <MapPin className="size-4 shrink-0" />
          <span className="truncate">{job.location || t('Place not given')}</span>
        </span>
        <b className="text-[1.06rem]">{k === null ? '–' : t('{n} km from home', { n: k })}</b>
      </Card>
    </div>
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
        <Button variant="ghost" className="h-10 text-muted-foreground">
          {t('Move to…')}
          <ChevronDown />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel>{t('Move this job to')}</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={job.status} onValueChange={v => v !== job.status && onMove(job, v as Status)}>
          {STAGES.map(s => (
            <DropdownMenuRadioItem key={s.id} value={s.id} className="h-10 text-base">
              {t(s.name)}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/** The one main action for the job's stage. */
function MainAction({ job, onMove }: { job: Job; onMove: Move }) {
  const { t } = useTranslation()
  const [asking, setAsking] = useState(false)
  useEffect(() => setAsking(false), [job.id])
  const big = 'h-auto min-h-14 w-full py-3 text-lg whitespace-normal'

  if (isOpen(job)) {
    if (asking || !job.url) {
      return (
        <div className="space-y-2 text-center">
          <p className="text-lg font-semibold">{t('Did you send your CV?')}</p>
          <div className="flex gap-2">
            <Button className="h-14 flex-1 text-lg" onClick={() => onMove(job, 'applied', { next: true })}>
              {t('Yes, I applied')}
            </Button>
            {job.url && (
              <Button variant="outline" className="h-14 flex-1 text-lg" onClick={() => setAsking(false)}>
                {t('Not yet')}
              </Button>
            )}
          </div>
        </div>
      )
    }
    return (
      <Button asChild className={big}>
        <a href={job.url} target="_blank" rel="noopener noreferrer" onClick={() => setAsking(true)}>
          {t('Open the posting to apply')}
          <ArrowUpRight />
        </a>
      </Button>
    )
  }
  const next: Partial<Record<Status, [Status, string]>> = {
    applied: ['interview', N_('The company called me for an interview')],
    interview: ['offer', N_('I got an offer')],
    closed: ['saved', N_('Put it back in Saved')],
  }
  const n = next[job.status]
  if (!n) return null
  return (
    <Button className={big} onClick={() => onMove(job, n[0])}>
      {t(n[1])}
    </Button>
  )
}

export function JobDetail({ job, phone, onBack, onMove }: {
  job: Job | null
  phone: boolean
  onBack: () => void
  onMove: Move
}) {
  const { t } = useTranslation()
  if (!job) {
    return (
      <div className="flex flex-1 items-center justify-center p-8 text-center text-muted-foreground">
        {t('Nothing here yet. Press “Add job” to add one.')}
      </div>
    )
  }
  const early = isOpen(job)
  const found = new Date(job.saved_at || job.created_at)
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
        <div>
          <h1 className="text-[1.75rem] leading-tight font-semibold tracking-tight">{job.title}</h1>
          <p className="text-[1.02rem] text-muted-foreground">{job.company}</p>
        </div>
        <NextStep job={job} />
        {early ? (
          <>
            <GradeCard job={job} />
            <Facts job={job} />
            <Reasons job={job} />
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
              <CollapsibleContent className="pt-3">
                <Reasons job={job} />
              </CollapsibleContent>
            </Collapsible>
          </>
        )}
        {job.score_status === 'failed' && (
          <p className="flex items-center gap-2 text-amber-800">
            <CircleAlert className="size-4" />
            {t("Couldn't rate this job. braveduck tries again later.")}
          </p>
        )}
        <p className="text-sm text-muted-foreground">
          {t('Found on {source}, {date}', {
            source: job.discovery_source ? t(sourceName(job.discovery_source)) : t('your list'),
            date: found.toLocaleDateString(locale(), { day: 'numeric', month: 'long' }),
          })}
          {' · '}
          {t(stageName(job.status))}
        </p>
      </div>
      <div className={cn('space-y-1 border-t bg-background px-5 py-3 md:px-8', phone && 'sticky bottom-0')}>
        <MainAction job={job} onMove={onMove} />
        <div className="flex items-center justify-between">
          {job.status === 'saved' ? (
            <Button variant="link" className="h-10 px-0 text-muted-foreground" onClick={() => onMove(job, 'closed', { next: true })}>
              {t('Not a fit, skip it')}
            </Button>
          ) : (
            <span />
          )}
          <MoveMenu job={job} onMove={onMove} />
        </div>
      </div>
    </section>
  )
}
