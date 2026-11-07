// "Are you sure?" as a window of the board's own (not window.confirm):
// const ok = await confirm({ title, body, ok }) anywhere, with one
// <ConfirmHost /> on the page.
import { useSyncExternalStore } from 'react'
import { useTranslation } from 'react-i18next'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'

interface Ask {
  title: string
  body?: string
  ok: string
  danger?: boolean
  done: (yes: boolean) => void
}

let current: Ask | null = null
const listeners = new Set<() => void>()
const set = (a: Ask | null) => {
  current = a
  listeners.forEach(l => l())
}

export function confirm(a: Omit<Ask, 'done'>): Promise<boolean> {
  current?.done(false)
  return new Promise(done => set({ ...a, done }))
}

export function ConfirmHost() {
  const { t } = useTranslation()
  const a = useSyncExternalStore(
    l => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    () => current,
  )
  const answer = (yes: boolean) => {
    a?.done(yes)
    set(null)
  }
  return (
    <AlertDialog open={!!a} onOpenChange={o => !o && answer(false)}>
      <AlertDialogContent className="sm:max-w-md">
        <AlertDialogHeader>
          <AlertDialogTitle className="text-lg">{a?.title}</AlertDialogTitle>
          {a?.body ? <AlertDialogDescription className="text-base">{a.body}</AlertDialogDescription> : <AlertDialogDescription className="sr-only">{a?.title}</AlertDialogDescription>}
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel className="h-11 text-base" onClick={() => answer(false)}>
            {t('Cancel')}
          </AlertDialogCancel>
          <AlertDialogAction variant={a?.danger ? 'destructive' : 'default'} className="h-11 text-base" onClick={() => answer(true)}>
            {a?.ok}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
