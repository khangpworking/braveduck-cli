// "Ask your AI": braveduck runs no AI itself for CVs and emails. This shows
// the request to paste into an agent connected to braveduck (or the whole
// brief, for any AI chat), and takes back what the AI wrote.
import { useEffect, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Copy } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import type { Job } from '@/lib/api'
import { saveCV, saveDraft, splitEmail, type Ask } from '@/lib/jobApi'
import { N_ } from '@/lib/n'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Textarea } from '@/components/ui/textarea'

export type AskKind = 'rate' | 'cv' | 'draft'

const TITLES: Record<AskKind, string> = {
  rate: N_('Rate this job with your AI'),
  cv: N_('Tailor your CV with your AI'),
  draft: N_('Draft the email with your AI'),
}

export function copy(text: string, t: (k: string) => string) {
  return navigator.clipboard.writeText(text).then(
    () => toast(t('Copied.')),
    () => toast.error(t("Couldn't copy. Select the text and copy it yourself.")),
  )
}

export function AskAI({ job, kind, ask, onClose }: { job: Job; kind: AskKind; ask: Ask | null; onClose: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [text, setText] = useState('')
  useEffect(() => setText(''), [ask])
  const save = useMutation({
    mutationFn: () => (kind === 'cv' ? saveCV(job.id, text) : saveDraft(job.id, splitEmail(text))),
    onSuccess: () => {
      void qc.invalidateQueries()
      toast(kind === 'cv' ? t('CV saved.') : t('Draft saved. Check it before you send it.'))
      onClose()
    },
    onError: (e: Error) => toast.error(t("Couldn't save: {error}", { error: t(e.message) })),
  })
  return (
    <Dialog open={!!ask} onOpenChange={o => !o && onClose()}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t(TITLES[kind])}</DialogTitle>
          <DialogDescription>
            {t('braveduck doesn’t run an AI for this. Paste this into Claude Code (with braveduck connected):')}
          </DialogDescription>
        </DialogHeader>
        <pre className="max-h-40 overflow-auto rounded-lg border bg-muted/50 p-3 text-sm whitespace-pre-wrap">{ask?.prompt}</pre>
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" className="h-11" onClick={() => ask && void copy(ask.prompt, t)}>
            <Copy />
            {t('Copy the request')}
          </Button>
          <Button variant="outline" className="h-11" onClick={() => ask && void copy(ask.brief, t)}>
            <Copy />
            {t('Copy everything (for any AI chat)')}
          </Button>
        </div>
        {kind !== 'rate' && (
          <>
            <Textarea
              value={text}
              onChange={e => setText(e.target.value)}
              aria-label={t('What your AI wrote')}
              placeholder={
                kind === 'cv'
                  ? t('…or paste the CV your AI wrote (Markdown) here')
                  : t('…or paste the email here: first line “Subject: …”, then the body')
              }
              className="min-h-28"
            />
            <DialogFooter>
              <Button className="h-11" disabled={!text.trim() || save.isPending} onClick={() => save.mutate()}>
                {t('Save what my AI wrote')}
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}
