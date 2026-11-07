// Settings: a side panel listing every settings window, big rows, one tap
// each. Any part of the board opens a window with openWindow(name) (or the
// "braveduck:open" event); this panel hosts them all, one at a time.
import { useCallback, useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Bell,
  Bot,
  ChevronRight,
  Download,
  FileText,
  Globe,
  History,
  KeyRound,
  ListChecks,
  MapPin,
  Search,
  Star,
  UserRound,
  Users,
  X,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { getJobs } from '@/lib/api'
import { LANGS, lang, locale, setLang, type Lang } from '@/lib/i18n'
import { MAX_NOTIFIED, canNotify, freshMatches, markSeen, notifyOn, seenIds, setNotify } from '@/lib/notify'
import { OPEN_EVENT, Q, S, download, hostedOf, type WindowName } from '@/lib/settings'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import { Sheet, SheetClose, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'
import { ActivityWindow } from './ActivityWindow'
import { AgentWindow } from './AgentWindow'
import { CVWindow } from './CVWindow'
import { ConfirmHost } from './Confirm'
import { FacebookWindow } from './FacebookWindow'
import { KeywordsWindow } from './KeywordsWindow'
import { LocationWindow } from './LocationWindow'
import { LoginsWindow } from './LoginsWindow'
import { ProfileWindow } from './ProfileWindow'
import { RatingWindow } from './RatingWindow'
import { SourcesWindow } from './SourcesWindow'
import { Body, Win, useErr, type WinProps } from './parts'

function Row({ icon: Icon, label, detail, bad, onClick }: {
  icon: React.ComponentType<{ className?: string }>
  label: string
  detail?: string
  bad?: boolean
  onClick: () => void
}) {
  return (
    <button type="button" onClick={onClick} className="flex min-h-12 w-full items-center gap-3 rounded-lg px-3 py-1.5 text-left hover:bg-muted">
      <Icon className="size-5 shrink-0 text-muted-foreground" />
      <span className="min-w-0 flex-1">
        <span className="block text-body font-medium">{label}</span>
        {detail && <span className={cn('block text-small', bad ? 'font-medium text-danger' : 'text-muted-foreground')}>{detail}</span>}
      </span>
      <ChevronRight className="size-5 shrink-0 text-muted-foreground" />
    </button>
  )
}

function LanguageWindow(p: WinProps) {
  const { t } = useTranslation()
  return (
    <Win {...p} title={t('Language')}>
      <Body>
        <div className="grid gap-2" role="group" aria-label={t('Language')}>
          {(Object.keys(LANGS) as Lang[]).map(l => (
            <Button
              key={l}
              lang={l}
              aria-pressed={l === lang}
              variant={l === lang ? 'default' : 'outline'}
              className="h-12 justify-start"
              onClick={() => (l === lang ? p.onOpenChange(false) : setLang(l))}
            >
              {LANGS[l]}
            </Button>
          ))}
        </div>
      </Body>
    </Win>
  )
}

/**
 * Great-match notifications, after each jobs refresh: up to three new A or B
 * jobs, best first, then one line for the rest. A click opens the job.
 */
function useMatchNotifier(onOpenJob: (id: number) => void) {
  const { t } = useTranslation()
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
  return jobs.data ?? []
}

export function Settings({ open, onOpenChange, onOpenJob }: {
  open: boolean
  onOpenChange: (o: boolean) => void
  onOpenJob: (id: number) => void
}) {
  const { t } = useTranslation()
  const err = useErr()
  const [win, setWin] = useState<WindowName | null>(null)
  const [notify, setNotifyState] = useState(notifyOn)
  const jobs = useMatchNotifier(onOpenJob)
  const setup = useQuery({ queryKey: Q.setup, queryFn: S.setup }).data
  const status = useQuery({ queryKey: Q.crawlStatus, queryFn: S.crawlStatus }).data
  const hosted = hostedOf(setup)
  const unhealthy = (status?.sources ?? []).filter(s => !s.healthy).length

  const show = useCallback(
    (w: WindowName) => {
      onOpenChange(false)
      setWin(w)
    },
    [onOpenChange],
  )
  useEffect(() => {
    const on = (e: Event) => show((e as CustomEvent<WindowName>).detail)
    addEventListener(OPEN_EVENT, on)
    return () => removeEventListener(OPEN_EVENT, on)
  }, [show])

  const toggleNotify = async () => {
    if (notifyOn()) {
      setNotify(false)
      toast(t('Match notifications off'))
    } else if (!canNotify()) {
      toast.error(t("This browser can't show notifications"))
    } else {
      const perm = Notification.permission === 'granted' ? 'granted' : await Notification.requestPermission()
      if (perm !== 'granted') {
        toast.error(t('Notifications are blocked for this page in your browser settings'))
      } else {
        setNotify(true)
        markSeen(jobs) // only matches rated from now on
        toast.success(t("You'll be notified of new A and B matches while the board is open"))
      }
    }
    setNotifyState(notifyOn())
  }

  const win_ = (w: WindowName): WinProps => ({ open: win === w, onOpenChange: o => !o && setWin(null) })

  return (
    <>
      <Sheet open={open} onOpenChange={onOpenChange}>
        <SheetContent side="right" showCloseButton={false} flush className="overflow-y-auto data-[side=right]:w-full data-[side=right]:sm:max-w-md">
          <SheetClose asChild>
            <Button variant="ghost" size="icon" className="absolute top-3 right-3 size-10" aria-label={t('Close')}>
              <X className="size-5" />
            </Button>
          </SheetClose>
          <SheetHeader bar>
            <SheetTitle>{t('Settings')}</SheetTitle>
            {hosted ? (
              <SheetDescription>
                <span className="mr-3">{t('Logged in as {user}', { user: hosted.user })}</span>
                {hosted.logout_url && /^https?:\/\//i.test(hosted.logout_url) && (
                  <a href={hosted.logout_url} className="font-medium text-primary underline underline-offset-4">
                    {t('Log out')}
                  </a>
                )}
              </SheetDescription>
            ) : (
              <SheetDescription className="sr-only">{t('Settings')}</SheetDescription>
            )}
          </SheetHeader>
          <nav className="flex flex-col gap-0.5 p-2" aria-label={t('Settings')}>
            <Row icon={UserRound} label={t('Your profile')} onClick={() => show('profile')} />
            <Row icon={FileText} label={t('My CV')} onClick={() => show('cv')} />
            <Row icon={Search} label={t('Search terms')} onClick={() => show('keywords')} />
            <Row
              icon={ListChecks}
              label={t('Job sources')}
              detail={unhealthy ? t('{n} need a look', { n: unhealthy }) : undefined}
              bad={unhealthy > 0}
              onClick={() => show('sources')}
            />
            <Row icon={MapPin} label={t('Your location')} onClick={() => show('location')} />
            <Row icon={Star} label={t('Rating')} onClick={() => show('rating')} />
            <Row icon={KeyRound} label={t('Job-site logins')} onClick={() => show('logins')} />
            <Row icon={Users} label={t('Facebook groups')} onClick={() => show('facebook')} />
            <Row icon={History} label={t('Recent activity')} onClick={() => show('activity')} />
            {hosted && <Row icon={Bot} label={t('Connect your AI agent')} onClick={() => show('agent')} />}
            <Separator className="my-2" />
            <Row
              icon={Download}
              label={t('Download my jobs (CSV)')}
              onClick={() => void download('/export.csv', 'braveduck-jobs.csv').catch(e => toast.error(err(e)))}
            />
            <Row icon={Globe} label={t('Language')} detail={LANGS[lang]} onClick={() => show('language')} />
            <label className="flex min-h-12 w-full cursor-pointer items-center gap-3 rounded-lg px-3 py-1.5 hover:bg-muted">
              <Bell className="size-5 shrink-0 text-muted-foreground" />
              <span className="min-w-0 flex-1 text-body font-medium">{t('Notify me of great matches')}</span>
              <Switch checked={notify} onCheckedChange={() => void toggleNotify()} className="scale-125" />
            </label>
          </nav>
        </SheetContent>
      </Sheet>
      <ProfileWindow {...win_('profile')} />
      <CVWindow {...win_('cv')} />
      <KeywordsWindow {...win_('keywords')} />
      <SourcesWindow {...win_('sources')} />
      <LocationWindow {...win_('location')} />
      <RatingWindow {...win_('rating')} />
      <LoginsWindow {...win_('logins')} />
      <FacebookWindow {...win_('facebook')} />
      <ActivityWindow {...win_('activity')} />
      {hosted && <AgentWindow {...win_('agent')} />}
      <LanguageWindow {...win_('language')} />
      <ConfirmHost />
    </>
  )
}
