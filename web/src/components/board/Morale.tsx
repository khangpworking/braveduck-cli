// How the applications are going: the interview rate and the server's
// encouragement (service.Stats, the same numbers get_stats gives an agent).
// Shown with the applications, once at least one has an answer.
import { useQuery } from '@tanstack/react-query'
import { TrendingUp } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'
import { locale } from '@/lib/i18n'
import { N_ } from '@/lib/n'

interface Stats {
  resolved: number
  interviews: number
  pending: number
  interview_rate: number
  morale: string
}

// The server's lines (internal/core/jobs/morale.go); the Vietnamese ones are
// shown as written.
export const MORALE_TEXTS = [N_("You're a fucking superstar 🌟")]

export function Morale() {
  const { t } = useTranslation()
  const q = useQuery({ queryKey: ['stats'], queryFn: () => api<Stats>('GET', '/stats') })
  const s = q.data
  if (!s || s.resolved < 1) return null
  const n = (v: number) => v.toLocaleString(locale())
  return (
    <div className="flex items-center gap-3 rounded-xl border bg-muted/40 px-4 py-3">
      <TrendingUp className="size-5 shrink-0 text-emerald-700" />
      <div className="min-w-0">
        <p className="font-semibold">
          {s.resolved >= 3 && <span className="mr-2 text-emerald-700">{n(Math.round(s.interview_rate * 100))}%</span>}
          {t('{n} interviews from {resolved} answered applications · {pending} waiting', {
            n: n(s.interviews),
            resolved: n(s.resolved),
            pending: n(s.pending),
          })}
        </p>
        {s.morale && <p className="text-muted-foreground">{t(s.morale)}</p>}
      </div>
    </div>
  )
}
