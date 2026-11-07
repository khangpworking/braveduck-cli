import { ChevronRight, Clock, MessageCircle, Pin, Star, Sun } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { Job } from '@/lib/api'
import type { TodayEntry } from '@/lib/jobs'
import { shortCompany } from '@/lib/jobs'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import { Company, GradeBadge, PayKm, Warning } from './bits'

export function JobRow({ job, current, onOpen }: { job: Job; current: boolean; onOpen: () => void }) {
  const { t } = useTranslation()
  return (
    <button
      type="button"
      data-job={job.id}
      onClick={onOpen}
      aria-current={current || undefined}
      className={cn(
        'flex w-full items-center gap-4 border-b border-l-4 border-l-transparent px-4 py-3.5 text-left transition-colors hover:bg-muted/60',
        current && 'border-l-primary bg-muted',
      )}
    >
      <GradeBadge job={job} />
      <span className="min-w-0 flex-1">
        <span className="flex items-center gap-1.5 text-body leading-snug font-semibold">
          {job.pinned && <Pin className="size-4 shrink-0 text-muted-foreground" aria-label={t('Pinned')} />}
          <span className="truncate">{job.title}</span>
        </span>
        <span className="block truncate text-small text-muted-foreground">
          <Company job={job} />
        </span>
        <span className="block text-body">
          <PayKm job={job} />
        </span>
        <Warning job={job} />
      </span>
    </button>
  )
}

const KIND_ICON = { follow: MessageCircle, deadline: Clock, match: Star }

/** One line saying what to do, for a Today entry. */
export function useTodayText() {
  const { t } = useTranslation()
  return (e: TodayEntry) => {
    const c = e.job.clock
    switch (e.kind) {
      case 'follow':
        return {
          title: t('Ask {company} for an answer', { company: shortCompany(e.job.company) }),
          detail: t('No answer for {n} days · {title}', { n: c?.days ?? 7, title: e.job.title }),
        }
      case 'deadline': {
        const d = c?.days ?? 0
        return {
          title: d <= 0 ? t('Closes today') : d === 1 ? t('Closes tomorrow') : t('{n} days left to apply', { n: d }),
          detail: `${e.job.title} · ${shortCompany(e.job.company)}`,
        }
      }
      default:
        return { title: t('A great fit for you'), detail: `${e.job.title} · ${shortCompany(e.job.company)}` }
    }
  }
}

export function TodayCard({ items, cap, onOpen, onAll }: {
  items: TodayEntry[]
  cap: number
  onOpen: (j: Job) => void
  onAll: () => void
}) {
  const { t } = useTranslation()
  const text = useTodayText()
  if (!items.length) return null
  return (
    <Card size="sm" tone="warning" aria-label={t('Today')}>
      <div className="flex flex-col">
      <div className="flex items-center justify-between px-4 py-1.5">
        <span className="flex items-center gap-2 text-body font-semibold">
          <Sun className="size-5 text-warning" />
          {t('Today')}
        </span>
        {items.length > cap && (
          <Button variant="quiet" className="h-10" onClick={onAll}>
            {t('See all ({n})', { n: items.length })}
            <ChevronRight />
          </Button>
        )}
      </div>
      {items.slice(0, cap).map(it => {
        const Icon = KIND_ICON[it.kind]
        const { title, detail } = text(it)
        return (
          <button
            type="button"
            key={it.job.id}
            onClick={() => onOpen(it.job)}
            className="mx-1.5 flex min-h-14 items-center gap-3 rounded-lg px-2.5 py-2 text-left hover:bg-warning-border/30"
          >
            <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-white shadow-xs">
              <Icon className="size-5 text-warning" />
            </span>
            <span className="min-w-0 flex-1">
              <span className="block font-semibold">{title}</span>
              <span className="block truncate text-small text-muted-foreground">{detail}</span>
            </span>
            <ChevronRight className="size-5 text-muted-foreground" />
          </button>
        )
      })}
      </div>
    </Card>
  )
}
