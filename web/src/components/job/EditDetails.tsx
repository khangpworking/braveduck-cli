import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { Job } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import type { JobActions } from './useJobActions'

export function EditDetails({ job, a, open, onOpenChange }: { job: Job; a: JobActions; open: boolean; onOpenChange: (o: boolean) => void }) {
  const { t } = useTranslation()
  const [v, setV] = useState({ title: '', company: '', salary: '', location: '' })
  useEffect(() => {
    if (open) setV({ title: job.title, company: job.company, salary: job.salary, location: job.location })
  }, [open, job])
  const field = (k: keyof typeof v, label: string) => (
    <label className="grid gap-1 text-sm font-medium">
      {label}
      <Input value={v[k]} onChange={e => setV({ ...v, [k]: e.target.value })} className="h-11" />
    </label>
  )
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t('Edit details')}</DialogTitle>
        </DialogHeader>
        <div className="grid gap-3">
          {field('title', t('Title'))}
          {field('company', t('Company'))}
          {field('salary', t('Pay'))}
          {field('location', t('Where'))}
        </div>
        <DialogFooter>
          <Button
            className="h-11"
            disabled={!v.title.trim() || a.edit.isPending}
            onClick={() => a.edit.mutate(v, { onSuccess: () => onOpenChange(false) })}
          >
            {t('Save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
