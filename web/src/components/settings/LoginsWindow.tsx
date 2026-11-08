// Job-site logins: each site's login, and the one thing to do about it. A
// login opens a browser window on this computer, so on a shared server
// there's nothing to connect, only saved logins to delete.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Q, S, hostedOf, type Platform } from '@/lib/settings'
import { N_ } from '@/lib/n'
import { locale } from '@/lib/i18n'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { SiteIcon } from '@/components/SiteIcon'
import { confirm } from './Confirm'
import { Body, CloseButton, Footer, Loading, Problem, Win, useErr, useFresh, type WinProps } from './parts'

const STATUS: Record<Platform['status'], [string, string]> = {
  not_connected: [N_('Not connected'), 'text-muted-foreground'],
  connecting: [N_('Waiting for you to log in in the browser window…'), 'text-warning'],
  connected: [N_('Logged in ✓'), 'text-success'],
  expired: [N_('Logged out — connect again'), 'text-danger'],
  unknown: [N_('Saved (login check not verified yet)'), 'text-warning'],
}

export function LoginsWindow(p: WinProps) {
  const { t } = useTranslation()
  // On a shared server login windows would open on the server, except for
  // its owner, whose computer it is.
  const h = hostedOf(useQuery({ queryKey: Q.setup, queryFn: S.setup }).data)
  const hosted = !!h && !h.owner
  return (
    <Win
      {...p}
      title={t('Job-site logins')}
      intro={
        hosted
          ? t("Logging in to a job site opens a browser window on your own computer, so it's off on this shared server. Run braveduck on your computer to apply through your accounts.")
          : t('Connect opens a browser window on this computer. You log in there yourself; braveduck never sees your password and keeps only the session, encrypted.')
      }
    >
      <LoginsBody hosted={hosted} />
      <Footer>
        <CloseButton />
      </Footer>
    </Win>
  )
}

function LoginsBody({ hosted }: { hosted: boolean }) {
  const { t } = useTranslation()
  const err = useErr()
  const qc = useQueryClient()
  // A login finishing changes the data, and live updates refetch this.
  const q = useFresh(Q.platforms, S.platforms)
  const done = () => {
    for (const k of [Q.platforms, Q.setup]) void qc.invalidateQueries({ queryKey: k })
  }
  const connect = useMutation({
    mutationFn: S.connect,
    onSuccess: () => toast(t('A browser window opened — log in there.')),
    onError: e => toast.error(t("Couldn't start login: {error}", { error: err(e) })),
    onSettled: done,
  })
  const sync = useMutation({
    mutationFn: S.sync,
    onSuccess: r => {
      const n = (v: number) => v.toLocaleString(locale())
      toast.success(t('TopCV: {found} applications — {updated} updated, {moved} moved to Applied, {added} added', { found: n(r.found), updated: n(r.updated), moved: n(r.moved), added: n(r.added) }))
      void qc.invalidateQueries({ queryKey: ['jobs'] })
    },
    onError: e => toast.error(t('Sync failed: {error}', { error: err(e) })),
    onSettled: done,
  })
  const disconnect = useMutation({
    mutationFn: async (id: string) => {
      if (!(await confirm({ title: t('Delete this saved login?'), ok: t('Delete'), danger: true }))) return false
      await S.disconnect(id)
      return true
    },
    onSuccess: yes => yes && toast.success(t('Login deleted.')),
    onError: e => toast.error(t("Couldn't disconnect: {error}", { error: err(e) })),
    onSettled: done,
  })

  if (q.isError) return <Body><Problem>{err(q.error)}</Problem></Body>
  if (!q.ready) return <Body><Loading /></Body>
  return (
    <Body>
      <div>
        {q.data.map(p => {
          const [label, tone] = STATUS[p.status] ?? [p.status, 'text-muted-foreground']
          const saved = p.status !== 'not_connected' && p.status !== 'connecting'
          return (
            <div key={p.id} data-platform={p.id} className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-b py-3 last:border-b-0">
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-x-2 font-semibold">
                  <SiteIcon id={p.id} className="size-5" />
                  {p.name} {p.account && <span className="font-normal text-muted-foreground">{p.account}</span>}
                </div>
                <div className={cn('text-small', tone)}>{t(label)}</div>
              </div>
              {p.status !== 'connecting' && (
                <div className="flex flex-wrap gap-2">
                  {!hosted && p.id === 'topcv' && p.status === 'connected' && (
                    <Button variant="outline" className="h-11" disabled={sync.isPending} onClick={() => sync.mutate(p.id)}>
                      {sync.isPending ? t('Syncing…') : t('Sync applications')}
                    </Button>
                  )}
                  {saved && (
                    <Button variant="outline" className="h-11" disabled={disconnect.isPending} onClick={() => disconnect.mutate(p.id)}>
                      {t('Disconnect')}
                    </Button>
                  )}
                  {!hosted && (
                    <Button
                      variant={saved ? 'outline' : 'default'}
                      className="h-11"
                      disabled={connect.isPending}
                      onClick={() => connect.mutate(p.id)}
                    >
                      {saved ? t('Log in again') : t('Connect')}
                    </Button>
                  )}
                </div>
              )}
            </div>
          )
        })}
      </div>
    </Body>
  )
}
