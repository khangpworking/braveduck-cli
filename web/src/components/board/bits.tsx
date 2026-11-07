// Small pieces shared by the list and the detail.
import { Ban } from 'lucide-react'
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
          <span key={d} className="inline-flex items-center gap-1 text-[0.95rem] font-semibold text-red-700">
            <Ban className="size-4" />
            {t(key, vars ?? {})}
          </span>
        )
      })}
    </span>
  )
}

export const Company = ({ job }: { job: Job }) => <>{shortCompany(job.company)}</>
