// Get started: the steps rating and the job search still need, at the top
// of the list until they're done. Each step is one big button opening its
// window.
import { useQuery } from '@tanstack/react-query'
import { ChevronRight, Rocket } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Q, S, openWindow, stepsLeft } from '@/lib/settings'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'

export function GetStarted() {
  const { t } = useTranslation()
  const setup = useQuery({ queryKey: Q.setup, queryFn: S.setup }).data
  const left = setup ? stepsLeft(setup) : []
  if (!left.length) return null
  return (
    <Card className="gap-2 border-sky-200 bg-sky-50/70 px-3 py-3" aria-label={t('Get started')} data-get-started>
      <div className="flex items-center justify-between gap-2 px-1">
        <span className="flex items-center gap-2 text-[1.06rem] font-semibold">
          <Rocket className="size-5 text-sky-700" />
          {t('Get started')}
        </span>
        <span className="text-[0.95rem] text-muted-foreground">
          {left.length === 1 ? t('{n} step left', { n: 1 }) : t('{n} steps left', { n: left.length })}
        </span>
      </div>
      <ol className="space-y-2">
        {left.map((s, i) => (
          <li key={s.key}>
            <Button
              variant={i === 0 ? 'default' : 'outline'}
              className="h-auto min-h-12 w-full justify-between py-2 text-left text-base whitespace-normal"
              onClick={() => openWindow(s.window)}
            >
              <span>
                {i + 1}. {t(s.title)}
              </span>
              <ChevronRight className="size-5" />
            </Button>
          </li>
        ))}
      </ol>
    </Card>
  )
}
