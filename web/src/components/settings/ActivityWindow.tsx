// Recent activity: what braveduck and you did lately, newest first, in the
// board's language.
import { CircleAlert, CircleCheck, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Q, S } from '@/lib/settings'
import { activityText } from '@/lib/activity'
import { ago } from '@/lib/schedule'
import { locale } from '@/lib/i18n'
import { Body, CloseButton, Footer, Loading, Problem, Win, useErr, useFresh, useMsg, type WinProps } from './parts'

export function ActivityWindow(p: WinProps) {
  const { t } = useTranslation()
  return (
    <Win {...p} title={t('Recent activity')}>
      <ActivityBody />
      <Footer>
        <CloseButton />
      </Footer>
    </Win>
  )
}

function ActivityBody() {
  const { t } = useTranslation()
  const err = useErr()
  const msg = useMsg()
  const q = useFresh(Q.activity, S.activity)
  if (q.isError) return <Body><Problem>{err(q.error)}</Problem></Body>
  if (!q.ready) return <Body><Loading /></Body>
  if (!q.data.length) return <Body><p className="text-muted-foreground">{t('Nothing yet.')}</p></Body>
  return (
    <Body>
      <ul data-activity>
        {q.data.slice(0, 20).map((a, i) => {
          const Icon = a.type === 'error' ? CircleAlert : a.type === 'info' ? RefreshCw : CircleCheck
          return (
            <li key={i} className="flex gap-3 border-b py-2.5 last:border-b-0">
              <Icon className={a.type === 'error' ? 'mt-0.5 size-5 shrink-0 text-danger' : 'mt-0.5 size-5 shrink-0 text-muted-foreground'} />
              <span className="min-w-0 flex-1 wrap-anywhere">{activityText(a.message, (k, v) => t(k, v ?? {}), locale())}</span>
              <time dateTime={a.timestamp} className="shrink-0 text-small text-muted-foreground">
                {msg(ago(a.timestamp))}
              </time>
            </li>
          )
        })}
      </ul>
    </Body>
  )
}
