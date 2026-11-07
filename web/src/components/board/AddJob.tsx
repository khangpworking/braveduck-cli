import { useEffect, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { ApiError, addContent, type Job } from '@/lib/api'
import { stageName } from '@/lib/jobs'
import { N_ } from '@/lib/n'
import { serverError } from '@/lib/serverText'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Textarea } from '@/components/ui/textarea'

/** What the pasted text looks like, said before anything is sent. */
export function detect(v: string): { ok: boolean; msg: string } | null {
  v = v.trim()
  if (!v) return null
  if (/facebook\.com\/groups\//i.test(v)) return { ok: true, msg: N_('A Facebook group: braveduck will look for jobs in it every day') }
  if (/^https?:\/\/\S+$/i.test(v)) return { ok: true, msg: N_('A link to a job posting: ready to add') }
  return v.length >= 20 ? { ok: true, msg: N_('The text of a posting: ready to add') } : { ok: false, msg: N_('Paste a little more') }
}

interface Duplicate {
  id: number
  title: string
  status: Job['status']
}

export function AddJob({ open, onOpenChange, onAdded }: {
  open: boolean
  onOpenChange: (o: boolean) => void
  onAdded: (id: number) => void
}) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [v, setV] = useState('')
  const [dups, setDups] = useState<Duplicate[]>([])
  const [err, setErr] = useState('')
  useEffect(() => {
    if (open) {
      setV('')
      setDups([])
      setErr('')
    }
  }, [open])
  const add = useMutation({
    mutationFn: addContent,
    onSuccess: res => {
      void qc.invalidateQueries()
      onOpenChange(false)
      if ('group' in res) {
        toast(res.added ? t('Group added. braveduck looks for new jobs in it every day.') : t('You already follow this group.'))
        return
      }
      toast(t('Added to Saved'))
      onAdded(res.id)
    },
    onError: (e: Error) => {
      if (e instanceof ApiError && e.status === 409 && Array.isArray(e.body.duplicates)) {
        setDups(e.body.duplicates as Duplicate[])
        setErr('')
      } else {
        setErr(serverError(e.message, t))
      }
    },
  })
  const d = detect(v)
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t('Add a job')}</DialogTitle>
          <DialogDescription>{t('Paste a link, a Facebook group, or the text of a posting.')}</DialogDescription>
        </DialogHeader>
        <Textarea
          value={v}
          onChange={e => {
            setV(e.target.value)
            setDups([])
            setErr('')
          }}
          placeholder={t('Paste here')}
          aria-label={t('Link or text of the posting')}
          className="min-h-32"
          autoFocus
        />
        <p className="min-h-6 font-medium" role="status">
          {d && t(d.msg)}
        </p>
        {dups.length > 0 && (
          <div className="rounded-lg border border-warning-border bg-warning-muted p-3" role="alert">
            <p className="font-semibold">{t('This job is already on your board:')}</p>
            {dups.map(x => (
              <button
                type="button"
                key={x.id}
                className="block py-1 text-left font-medium text-primary underline-offset-4 hover:underline"
                onClick={() => {
                  onOpenChange(false)
                  onAdded(x.id)
                }}
              >
                {x.title} · {t(stageName(x.status))}
              </button>
            ))}
          </div>
        )}
        {err && (
          <p className="text-danger" role="alert">
            {err}
          </p>
        )}
        <Button className="h-12" disabled={!d?.ok || add.isPending} onClick={() => add.mutate(v.trim())}>
          {add.isPending ? t('Reading the posting…') : t('Add')}
        </Button>
      </DialogContent>
    </Dialog>
  )
}
