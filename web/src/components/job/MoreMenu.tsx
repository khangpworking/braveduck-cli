// The ⋯ menu on a job: pin, keep, edit details, delete.
import { lazy, useState } from 'react'
import { Ellipsis, LifeBuoy, Pencil, Pin, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { Job } from '@/lib/api'
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Deferred, WindowLoading } from '@/components/Deferred'
import {
  DropdownMenu, DropdownMenuCheckboxItem, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import type { JobActions } from './useJobActions'

const EditDetails = lazy(() => import('./EditDetails').then(m => ({ default: m.EditDetails })))

/** Keep matters only for jobs the clock would close by itself. */
export const keepable = (j: Job) => !!j.expires_at || j.status === 'applied' || j.status === 'interview'

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
          <DropdownMenuCheckboxItem checked={job.pinned} onCheckedChange={() => a.pin.mutate()} className="h-11">
            <Pin />
            {t('Pin to the top')}
          </DropdownMenuCheckboxItem>
          {keepable(job) && (
            <DropdownMenuCheckboxItem checked={job.keep} onCheckedChange={() => a.keep.mutate()} className="h-11">
              <LifeBuoy />
              {t('Keep (don’t move it to Closed by itself)')}
            </DropdownMenuCheckboxItem>
          )}
          <DropdownMenuItem onSelect={() => setEditing(true)} className="h-11">
            <Pencil />
            {t('Edit details')}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem variant="destructive" onSelect={() => setDeleting(true)} className="h-11">
            <Trash2 />
            {t('Delete')}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <Deferred active={editing} fallback={<WindowLoading title={t('Edit details')} onClose={() => setEditing(false)} />}>
        <EditDetails job={job} a={a} open={editing} onOpenChange={setEditing} />
      </Deferred>
      <AlertDialog open={deleting} onOpenChange={setDeleting}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Delete this job?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t('“{title}” and its notes, CVs and email go for good. To just put it away, move it to Closed.', { title: job.title })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel className="h-11">{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              className="h-11"
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
