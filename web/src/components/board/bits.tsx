// Small pieces shared by the list and the detail.
import { Ban, Clock, MessageCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { Job } from '@/lib/api'
import { GRADES, dealbreakerText, grade, km, pay, payUndisclosed, shortCompany } from '@/lib/jobs'
import { locale } from '@/lib/i18n'
import { cn } from '@/lib/utils'

export function GradeBadge({ job, size = 'md' }: { job: Job; size?: 'md' | 'lg' }) {
  const { t } = useTranslation()
  const g = grade(job)
  const label = job.score_status === 'pending' ? t('Rating…') : t(GRADES[g].label)
  return (
    <span
      title={label}
      aria-label={g === '?' ? label : t('Grade {g}: {label}', { g, label })}
      className={cn(
        'inline-flex shrink-0 items-center justify-center rounded-xl font-bold',
        GRADES[g].tone,
        size === 'lg' ? 'size-16 text-3xl' : 'size-12 text-2xl',
      )}
    >
      {g === '?' ? '–' : g}
    </span>
  )
}

/** "14–18 triệu · 2 km", or as much of it as is known. */
export function PayKm({ job }: { job: Job }) {
  const { t } = useTranslation()
  const k = km(job)
  const parts = [payUndisclosed(job.salary) ? t('Pay not given') : pay(job.salary, locale()), k === null ? '' : `${k} km`]
  return <>{parts.filter(Boolean).join(' · ')}</>
}

/** The first deal-breaker, in red, so a green letter never hides it. */
export function Warning({ job, all = false }: { job: Job; all?: boolean }) {
  const { t } = useTranslation()
  const list = job.dealbreakers ?? []
  if (!list.length) return null
  return (
    <span className="mt-1 flex flex-wrap gap-x-3 gap-y-1">
      {(all ? list : list.slice(0, 1)).map(d => {
        const [key, vars] = dealbreakerText(d)
        return (
          <span key={d} className="inline-flex items-center gap-1 text-small font-semibold text-danger">
            <Ban className="size-4" />
            {t(key, vars ?? {})}
          </span>
        )
      })}
    </span>
  )
}

export const Company = ({ job }: { job: Job }) => <>{shortCompany(job.company)}</>

/**
 * What's due, in the row itself rather than a banner: a posting closing within
 * a week, or an application a week without an answer (and no follow-up yet).
 */
export function Due({ job }: { job: Job }) {
  const { t } = useTranslation()
  const c = job.clock
  if (c?.kind === 'deadline' && c.days <= 7 && (job.status === 'saved' || job.status === 'waiting')) {
    const text = c.days < 0 ? t('This posting has closed') : c.days === 0 ? t('Closes today') : c.days === 1 ? t('Closes tomorrow') : t('{n} days left to apply', { n: c.days })
    return (
      <span className={cn('mt-1 inline-flex items-center gap-1 text-small font-semibold', c.days <= 1 ? 'text-danger' : 'text-warning')}>
        <Clock className="size-4" />
        {text}
      </span>
    )
  }
  if (c?.kind === 'applied' && job.status === 'applied' && c.days >= 7 && !job.followup_sent_at) {
    return (
      <span className="mt-1 inline-flex items-center gap-1 text-small font-semibold text-warning">
        <MessageCircle className="size-4" />
        {t('No answer for {n} days', { n: c.days })}
      </span>
    )
  }
  return null
}
