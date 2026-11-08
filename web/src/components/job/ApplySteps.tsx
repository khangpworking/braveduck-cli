import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { JobActions } from './useJobActions'

/** After "Apply on TopCV": what to do in the window that opened. */
export function ApplySteps({ a }: { a: JobActions }) {
  const { t } = useTranslation()
  const p = a.prep
  const steps = p
    ? [
        p.site_id === 'topcv' ? t('Click Apply (Ứng tuyển).') : t('Click Apply.'),
        t('Upload this CV: {file}', { file: p.cv_pdf }),
        ...(p.cover_letter ? [t('Paste the cover letter (it’s already copied).')] : []),
        t('Submit it, then close the window.'),
      ]
    : []
  return (
    <Dialog open={!!p} onOpenChange={o => !o && a.setPrep(null)}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t('A {site} window is opening', { site: p?.site ?? '' })}</DialogTitle>
          <DialogDescription>{t('You’re logged in there. You submit it yourself:')}</DialogDescription>
        </DialogHeader>
        <ol className="list-decimal space-y-2 pl-6 text-body">
          {steps.map(s => (
            <li key={s} className="break-words">
              {s}
            </li>
          ))}
        </ol>
        <p className="text-muted-foreground">
          {p?.site_id === 'topcv'
            ? t('braveduck checks TopCV afterwards and moves the job to Applied if it’s there.')
            : t('When you close the window, braveduck asks whether you submitted it.')}
        </p>
        <DialogFooter>
          <Button className="h-11" onClick={() => a.setPrep(null)}>
            {t('OK')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
