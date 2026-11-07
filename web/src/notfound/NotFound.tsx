// The page for an address braveduck doesn't have (served with status 404).
import { ArrowLeft, Home } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'

export function NotFound() {
  const { t } = useTranslation()
  document.title = t('Page not found · braveduck')
  return (
    <main className="relative flex min-h-dvh items-center justify-center overflow-hidden px-5">
      <div className="bg-grid pointer-events-none absolute inset-0" />
      <div className="pointer-events-none absolute inset-x-0 top-0 h-80 bg-linear-to-b from-brand-muted to-transparent" />
      <div className="relative max-w-md text-center">
        <p className="text-8xl" aria-hidden>
          🦆
        </p>
        <p className="mt-6 text-sm font-semibold tracking-widest text-warning uppercase">{t('Error 404')}</p>
        <h1 className="mt-2 text-3xl font-semibold tracking-tight md:text-4xl">{t('This page swam away')}</h1>
        <p className="mt-4 text-body text-muted-foreground">
          {t('The address may be mistyped, or the page has moved. Your jobs are safe on your board.')}
        </p>
        <div className="mt-8 flex flex-wrap justify-center gap-3">
          <Button asChild size="lg">
            <a href="/">
              <Home />
              {t('Back to my board')}
            </a>
          </Button>
          <Button size="lg" variant="outline" onClick={() => history.back()}>
            <ArrowLeft />
            {t('Go back')}
          </Button>
        </div>
        <p className="mt-10 text-sm text-muted-foreground">
          {t('New to braveduck?')}{' '}
          <a href="/welcome" className="font-medium text-foreground underline underline-offset-4">
            {t('See what it does')}
          </a>
        </p>
      </div>
    </main>
  )
}
