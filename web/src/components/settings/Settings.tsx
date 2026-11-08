import { lazy, useCallback, useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { getJobs } from '@/lib/api'
import { locale } from '@/lib/i18n'
import { MAX_NOTIFIED, freshMatches, markSeen, notifyOn, seenIds } from '@/lib/notify'
import { OPEN_EVENT, type WindowName } from '@/lib/settings'
import { Deferred, WindowLoading } from '@/components/Deferred'
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'

const SettingsContent = lazy(() => import('./SettingsContent').then(m => ({ default: m.SettingsContent })))

/** Notifications and cross-board window events must work before Settings opens. */
export function Settings({ open, onOpenChange, onOpenJob }: {
  open: boolean
  onOpenChange: (o: boolean) => void
  onOpenJob: (id: number) => void
}) {
  const { t } = useTranslation()
  const [win, setWin] = useState<WindowName | null>(null)
  const jobs = useQuery({ queryKey: ['jobs'], queryFn: getJobs })
  useEffect(() => {
    if (!jobs.data || !notifyOn()) return
    const fresh = freshMatches(jobs.data, seenIds())
    for (const j of fresh.slice(0, MAX_NOTIFIED)) {
      const score = Number(j.score).toLocaleString(locale(), { minimumFractionDigits: 1, maximumFractionDigits: 1 })
      const n = new Notification(t('Great match: {title}', { title: j.title }), {
        body: `${j.score_letter} · ${score}${j.company ? ` — ${j.company}` : ''}`,
        tag: `braveduck-job-${j.id}`,
      })
      n.onclick = () => {
        window.focus()
        onOpenJob(j.id)
        n.close()
      }
    }
    if (fresh.length > MAX_NOTIFIED) {
      const n = new Notification(t('{n} more great matches', { n: fresh.length - MAX_NOTIFIED }), {
        body: t('See Today on the board.'),
        tag: 'braveduck-more',
      })
      n.onclick = () => {
        window.focus()
        n.close()
      }
    }
    markSeen(jobs.data)
    // Only a new answer from the server counts, not a new callback.
  }, [jobs.dataUpdatedAt])

  const show = useCallback((w: WindowName) => {
    onOpenChange(false)
    setWin(w)
  }, [onOpenChange])
  useEffect(() => {
    const on = (e: Event) => show((e as CustomEvent<WindowName>).detail)
    addEventListener(OPEN_EVENT, on)
    return () => removeEventListener(OPEN_EVENT, on)
  }, [show])

  return (
    <Deferred active={open || win !== null} fallback={open ? (
      <Sheet open onOpenChange={onOpenChange}>
        <SheetContent side="right" flush className="data-[side=right]:w-full data-[side=right]:sm:max-w-md" aria-describedby={undefined} aria-busy>
          <SheetHeader bar><SheetTitle>{t('Settings')}</SheetTitle></SheetHeader>
          <div className="space-y-3 p-5" aria-hidden>
            <Skeleton className="h-11 w-full" />
            <Skeleton className="h-11 w-full" />
            <Skeleton className="h-11 w-full" />
          </div>
        </SheetContent>
      </Sheet>
    ) : <WindowLoading title={t('Settings')} onClose={() => setWin(null)} />}>
      <SettingsContent open={open} onOpenChange={onOpenChange} jobs={jobs.data ?? []} win={win} setWin={setWin} show={show} />
    </Deferred>
  )
}
