// Loaded on the first settings visit; each window loads on its own opening.
import { lazy, useState } from 'react'
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
import type { Job } from '@/lib/api'
import { LANGS, lang, setLang, type Lang } from '@/lib/i18n'
import { canNotify, markSeen, notifyOn, setNotify } from '@/lib/notify'
import { Q, S, download, hostedOf, type WindowName } from '@/lib/settings'
import { Deferred, WindowLoading } from '@/components/Deferred'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import { Sheet, SheetClose, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'
import { ConfirmHost } from './Confirm'
import { Body, Win, useErr, type WinProps } from './parts'

const ProfileWindow = lazy(() => import('./ProfileWindow').then(m => ({ default: m.ProfileWindow })))
const CVWindow = lazy(() => import('./CVWindow').then(m => ({ default: m.CVWindow })))
const KeywordsWindow = lazy(() => import('./KeywordsWindow').then(m => ({ default: m.KeywordsWindow })))
const SourcesWindow = lazy(() => import('./SourcesWindow').then(m => ({ default: m.SourcesWindow })))
const LocationWindow = lazy(() => import('./LocationWindow').then(m => ({ default: m.LocationWindow })))
const RatingWindow = lazy(() => import('./RatingWindow').then(m => ({ default: m.RatingWindow })))
const LoginsWindow = lazy(() => import('./LoginsWindow').then(m => ({ default: m.LoginsWindow })))
const FacebookWindow = lazy(() => import('./FacebookWindow').then(m => ({ default: m.FacebookWindow })))
const ActivityWindow = lazy(() => import('./ActivityWindow').then(m => ({ default: m.ActivityWindow })))
const AgentWindow = lazy(() => import('./AgentWindow').then(m => ({ default: m.AgentWindow })))

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

export function SettingsContent({ open, onOpenChange, jobs, win, setWin, show }: {
  open: boolean
  onOpenChange: (o: boolean) => void
  jobs: Job[]
  win: WindowName | null
  setWin: (w: WindowName | null) => void
  show: (w: WindowName) => void
}) {
  const { t } = useTranslation()
  const err = useErr()
  const [notify, setNotifyState] = useState(notifyOn)
  const setup = useQuery({ queryKey: Q.setup, queryFn: S.setup }).data
  const status = useQuery({ queryKey: Q.crawlStatus, queryFn: S.crawlStatus }).data
  const hosted = hostedOf(setup)
  const unhealthy = (status?.sources ?? []).filter(s => !s.healthy).length

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
      {([
        ['profile', ProfileWindow, t('Your profile')],
        ['cv', CVWindow, t('My CV')],
        ['keywords', KeywordsWindow, t('Search terms')],
        ['sources', SourcesWindow, t('Job sources')],
        ['location', LocationWindow, t('Your location')],
        ['rating', RatingWindow, t('How jobs get rated')],
        ['logins', LoginsWindow, t('Job-site logins')],
        ['facebook', FacebookWindow, t('Facebook groups you follow')],
        ['activity', ActivityWindow, t('Recent activity')],
        ...(hosted ? [['agent', AgentWindow, t('Connect your AI agent')] as const] : []),
      ] as const).map(([name, Window, title]) => (
        <Deferred key={name} active={win === name} fallback={<WindowLoading title={title} className={name === 'profile' ? 'sm:max-w-2xl' : 'sm:max-w-lg'} onClose={() => setWin(null)} />}>
          <Window {...win_(name)} />
        </Deferred>
      ))}
      <LanguageWindow {...win_('language')} />
      <ConfirmHost />
    </>
  )
}
