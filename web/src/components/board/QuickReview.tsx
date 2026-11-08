// "Go through them": one job per screen, three answers, the next job comes
// by itself. For working through a long list of new jobs without losing the
// thread: no windows open on top, the keys do it all.
import { useEffect, useState } from 'react'
import { ArrowUpRight, Check, ChevronRight, PartyPopper, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { Job, Status } from '@/lib/api'
import { GRADES, grade, postingUrl, reasons } from '@/lib/jobs'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Progress } from '@/components/ui/progress'
import { Due, GradeBadge, PayKm, Warning } from './bits'

interface Tally {
  applied: number
  skipped: number
  later: number
}

export function QuickReview({ jobs, open, onOpenChange, onMove }: {
  jobs: Job[]
  open: boolean
  onOpenChange: (o: boolean) => void
  onMove: (job: Job, to: Status) => void
}) {
  const { t } = useTranslation()
  const [queue, setQueue] = useState<Job[]>([])
  const [i, setI] = useState(0)
  const [asking, setAsking] = useState(false)
  const [tally, setTally] = useState<Tally>({ applied: 0, skipped: 0, later: 0 })
  // The list is taken when it opens, so jobs don't move under the reader.
  useEffect(() => {
    if (open) {
      setQueue(jobs)
      setI(0)
      setAsking(false)
      setTally({ applied: 0, skipped: 0, later: 0 })
    }
  }, [open]) // eslint-disable-line react-hooks/exhaustive-deps

  const job = queue[i]
  const next = (what: keyof Tally) => {
    setTally(n => ({ ...n, [what]: n[what] + 1 }))
    setAsking(false)
    setI(n => n + 1)
  }
  const later = () => job && next('later')
  const skip = () => {
    if (!job) return
    onMove(job, 'closed')
    next('skipped')
  }
  const applied = () => {
    if (!job) return
    onMove(job, 'applied')
    next('applied')
  }
  const openPosting = () => {
    const url = job && postingUrl(job)
    if (url) window.open(url, '_blank', 'noopener')
    setAsking(true)
  }

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.ctrlKey || e.metaKey || e.altKey) return
      const k = e.key.toLowerCase()
      if (k === 'enter' || k === 'o') openPosting()
      else if (k === 'a' || (asking && k === 'y')) applied()
      else if (k === 'x') skip()
      else if (k === 'arrowright' || k === 'l' || k === ' ') later()
      else return
      e.preventDefault()
    }
    addEventListener('keydown', onKey)
    return () => removeEventListener('keydown', onKey)
  })

  const total = queue.length
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl" aria-describedby={undefined}>
        <DialogHeader>
          <DialogTitle>{t('Go through your jobs')}</DialogTitle>
          {job && <DialogDescription>{t('{n} of {total}', { n: i + 1, total })}</DialogDescription>}
        </DialogHeader>
        <Progress value={total ? (Math.min(i, total) / total) * 100 : 0} aria-label={t('{n} of {total}', { n: Math.min(i + 1, total), total })} />
        {job ? (
          <div className="space-y-4" data-review-job={job.id}>
            <div className="flex items-start gap-4">
              <GradeBadge job={job} size="lg" />
              <div className="min-w-0">
                <h3 className="text-2xl leading-tight font-semibold">{job.title}</h3>
                <p className="text-muted-foreground">{job.company}</p>
                <p className="text-body font-medium">
                  <PayKm job={job} />
                </p>
                <span className="flex flex-wrap gap-x-3">
                  <Due job={job} />
                  <Warning job={job} all />
                </span>
              </div>
            </div>
            {grade(job) !== '?' && (
              <ul className="space-y-1.5 rounded-xl bg-muted/60 p-4">
                <li className="font-semibold">{t(GRADES[grade(job)].label)}</li>
                {reasons(job).slice(0, 3).map(r => (
                  <li key={r} className="flex items-start gap-2">
                    <ChevronRight className="mt-1 size-4 shrink-0 text-muted-foreground" />
                    {r}
                  </li>
                ))}
              </ul>
            )}
            {asking ? (
              <div className="space-y-2 text-center">
                <p className="text-lg font-semibold">{t('Did you send your CV?')}</p>
                <div className="grid grid-cols-2 gap-2">
                  <Button size="xl" onClick={applied}>
                    <Check />
                    {t('Yes, I applied')} <kbd className="opacity-60">Y</kbd>
                  </Button>
                  <Button size="xl" variant="outline" onClick={later}>
                    {t('Not yet, next job')} <kbd className="opacity-60">→</kbd>
                  </Button>
                </div>
              </div>
            ) : (
              <div className="grid gap-2 sm:grid-cols-3">
                <Button size="xl" onClick={openPosting}>
                  {t('Open to apply')}
                  <ArrowUpRight />
                </Button>
                <Button size="xl" variant="outline" onClick={later}>
                  {t('Later')} <kbd className="opacity-60">→</kbd>
                </Button>
                <Button size="xl" variant="outline" onClick={skip}>
                  <X />
                  {t('Not a fit')} <kbd className="opacity-60">X</kbd>
                </Button>
              </div>
            )}
            <p className="text-center text-sm text-muted-foreground">{t('Keys: Enter open · A applied · → later · X not a fit · Esc stop')}</p>
          </div>
        ) : (
          <div className="space-y-4 py-6 text-center" data-review-done>
            <PartyPopper className="mx-auto size-12 text-warning" />
            <p className="text-2xl font-semibold">{t('All done!')}</p>
            <p className="text-body text-muted-foreground">
              {t('You went through {n} jobs: applied to {a}, skipped {s}, kept {l} for later.', {
                n: total,
                a: tally.applied,
                s: tally.skipped,
                l: tally.later,
              })}
            </p>
            <Button size="lg" onClick={() => onOpenChange(false)}>
              {t('Back to the list')}
            </Button>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
