import { Suspense, useState, type ReactNode } from 'react'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Skeleton } from '@/components/ui/skeleton'

// Keep an opened component mounted so closing animations and unsaved state
// behave as before. A closed window must not start its dynamic import.
export function Deferred({ active, fallback, children }: { active: boolean; fallback: ReactNode; children: ReactNode }) {
  const [visited, setVisited] = useState(active)
  if (active && !visited) setVisited(true)
  if (!active && !visited) return null
  return <Suspense fallback={active ? fallback : null}>{children}</Suspense>
}

export function WindowLoading({ title, onClose, className }: { title: string; onClose: () => void; className?: string }) {
  return (
    <Dialog open onOpenChange={o => !o && onClose()}>
      <DialogContent className={className} aria-describedby={undefined} aria-busy>
        <DialogHeader><DialogTitle>{title}</DialogTitle></DialogHeader>
        <div className="space-y-3" aria-hidden>
          <Skeleton className="h-6 w-2/3" />
          <Skeleton className="h-11 w-full" />
          <Skeleton className="h-11 w-full" />
        </div>
      </DialogContent>
    </Dialog>
  )
}
