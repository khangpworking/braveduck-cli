// The ⋯ menu on a job: pin, keep, edit details, delete.
import { useEffect, useState } from 'react'
import { Ellipsis, LifeBuoy, Pencil, Pin, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { Job } from '@/lib/api'
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import {
  DropdownMenu, DropdownMenuCheckboxItem, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import type { JobActions } from './useJobActions'

/** Keep matters only for jobs the clock would close by itself. */
export const keepable = (j: Job) => !!j.expires_at || j.status === 'applied' || j.status === 'interview'

function EditDetails({ job, a, open, onOpenChange }: { job: Job; a: JobActions; open: boolean; onOpenChange: (o: boolean) => void }) {
  const { t } = useTranslation()
  const [v, setV] = useState({ title: '', company: '', salary: '', location: '' })
  useEffect(() => {
    if (open) setV({ title: job.title, company: job.company, salary: job.salary, location: job.location })
  }, [open, job])
  const field = (k: keyof typeof v, label: string) => (
    <label className="grid gap-1 text-sm font-medium">
      {label}
      <Input value={v[k]} onChange={e => setV({ ...v, [k]: e.target.value })} className="h-11 text-base" />
    </label>
  )
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="text-xl">{t('Edit details')}</DialogTitle>
        </DialogHeader>
        <div className="grid gap-3">
          {field('title', t('Title'))}
          {field('company', t('Company'))}
          {field('salary', t('Pay'))}
          {field('location', t('Where'))}
        </div>
        <DialogFooter>
          <Button
            className="h-11 text-base"
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

export function MoreMenu({ job, a, onDeleted }: { job: Job; a: JobActions; onDeleted: () => void }) {
  const { t } = useTranslation()
  const [editing, setEditing] = useState(false)
  const [deleting, setDeleting] = useState(false)
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="icon" className="size-11 shrink-0" aria-label={t('More actions')}>
            <Ellipsis className="size-5" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-64">
          <DropdownMenuCheckboxItem checked={job.pinned} onCheckedChange={() => a.pin.mutate()} className="h-11 text-base">
            <Pin />
            {t('Pin to the top')}
          </DropdownMenuCheckboxItem>
          {keepable(job) && (
            <DropdownMenuCheckboxItem checked={job.keep} onCheckedChange={() => a.keep.mutate()} className="h-11 text-base">
              <LifeBuoy />
              {t('Keep (don’t move it to Closed by itself)')}
            </DropdownMenuCheckboxItem>
          )}
          <DropdownMenuItem onSelect={() => setEditing(true)} className="h-11 text-base">
            <Pencil />
            {t('Edit details')}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem variant="destructive" onSelect={() => setDeleting(true)} className="h-11 text-base">
            <Trash2 />
            {t('Delete')}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <EditDetails job={job} a={a} open={editing} onOpenChange={setEditing} />
      <AlertDialog open={deleting} onOpenChange={setDeleting}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Delete this job?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t('“{title}” and its notes, CVs and email go for good. To just put it away, move it to Closed.', { title: job.title })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel className="h-11 text-base">{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              className="h-11 text-base"
              onClick={() => a.remove.mutate(undefined, { onSuccess: onDeleted })}
            >
              {t('Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
