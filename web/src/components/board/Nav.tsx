// The left menu on a desktop: adding a job, Today, the stages, Settings.
// It folds into a strip of icons to make room for the list and the job, and
// remembers whether it's folded.
import { useEffect, useState } from 'react'
import {
  Archive, Bookmark, MessagesSquare, PanelLeftClose, PanelLeftOpen, PenLine, Plus, Send, Settings as Gear, Sun, Trophy,
  type LucideIcon,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { Status } from '@/lib/api'
import { STAGES } from '@/lib/jobs'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

export type View = Status | 'today'

export const STAGE_ICONS: Record<View, LucideIcon> = {
  today: Sun,
  saved: Bookmark,
  waiting: PenLine,
  applied: Send,
  interview: MessagesSquare,
  offer: Trophy,
  closed: Archive,
}

const KEY = 'braveduck.menu'

/** Whether the menu is open, saved across visits; "[" opens and folds it. */
export function useMenuOpen() {
  const [open, setOpen] = useState(() => {
    try {
      return localStorage.getItem(KEY) !== 'folded'
    } catch {
      return true
    }
  })
  useEffect(() => {
    try {
      localStorage.setItem(KEY, open ? 'open' : 'folded')
    } catch {
      // nothing to save to
    }
  }, [open])
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement
      if (e.key !== '[' || e.ctrlKey || e.metaKey || e.altKey || el.closest('input, textarea, select, [contenteditable=true], [role=dialog]')) return
      e.preventDefault()
      setOpen(o => !o)
    }
    addEventListener('keydown', onKey)
    return () => removeEventListener('keydown', onKey)
  }, [])
  return [open, setOpen] as const
}

interface NavProps {
  view: View
  setView: (v: View) => void
  counts: Record<View, number>
  onAdd: () => void
  onSettings: () => void
  onFold: () => void
}

function NavItem({ active, onClick, label, count }: {
  active?: boolean
  onClick: () => void
  label: React.ReactNode
  count?: number
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-current={active || undefined}
      className={cn(
        'flex h-11 items-center justify-between rounded-lg px-3 text-left text-body hover:bg-muted',
        active && 'bg-primary font-semibold text-primary-foreground hover:bg-primary/90',
      )}
    >
      <span className="flex items-center gap-2">{label}</span>
      {count !== undefined && <span className="tabular-nums opacity-70">{count}</span>}
    </button>
  )
}

/** The open menu. */
export function Nav({ view, setView, counts, onAdd, onSettings, onFold }: NavProps) {
  const { t } = useTranslation()
  return (
    <nav className="flex w-56 shrink-0 flex-col gap-0.5 border-r bg-muted/40 p-3" aria-label={t('Stages')}>
      <div className="flex items-center gap-2 pt-1 pb-3 pl-2">
        <span className="flex-1 text-lg font-bold">🦆 braveduck</span>
        <Button variant="ghost" size="icon-sm" onClick={onFold} aria-label={t('Fold the menu away')} title={t('Fold the menu away ( [ )')}>
          <PanelLeftClose />
        </Button>
      </div>
      <Button className="mb-3 h-12" onClick={onAdd}>
        <Plus />
        {t('Add job')}
      </Button>
      <NavItem
        active={view === 'today'}
        onClick={() => setView('today')}
        label={
          <>
            <Sun className="size-4" />
            {t('Today')}
          </>
        }
        count={counts.today}
      />
      <div className="px-3 pt-4 pb-1 text-sm text-muted-foreground">{t('Your jobs')}</div>
      {STAGES.map(s => {
        const Icon = STAGE_ICONS[s.id]
        return (
          <NavItem
            key={s.id}
            active={view === s.id}
            onClick={() => setView(s.id)}
            label={
              <>
                <Icon className="size-4" />
                {t(s.name)}
              </>
            }
            count={counts[s.id]}
          />
        )
      })}
      <div className="flex-1" />
      <NavItem
        onClick={onSettings}
        label={
          <>
            <Gear className="size-4" />
            {t('Settings')}
          </>
        }
      />
    </nav>
  )
}

/** The folded menu as a strip of icons; each says what it is when pointed at. */
export function NavRail({ view, setView, counts, onAdd, onSettings, onFold }: NavProps) {
  const { t } = useTranslation()
  const item = (v: View, name: string) => {
    const Icon = STAGE_ICONS[v]
    const active = view === v
    return (
      <Tooltip key={v}>
        <TooltipTrigger asChild>
          <Button
            variant={active ? 'default' : 'ghost'}
            size="icon"
            className="relative"
            aria-current={active || undefined}
            aria-label={`${t(name)} (${counts[v]})`}
            onClick={() => setView(v)}
          >
            <Icon className="size-5" />
            {counts[v] > 0 && (
              <span className="absolute -top-0.5 -right-0.5 min-w-5 rounded-full border bg-background px-1 text-xs leading-4 font-semibold text-foreground tabular-nums">
                {counts[v]}
              </span>
            )}
          </Button>
        </TooltipTrigger>
        <TooltipContent side="right">
          {t(name)} · {counts[v]}
        </TooltipContent>
      </Tooltip>
    )
  }
  return (
    <TooltipProvider delayDuration={200}>
      <nav className="flex w-16 shrink-0 flex-col items-center gap-1.5 border-r bg-muted/40 py-3" aria-label={t('Stages')}>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="icon" onClick={onFold} aria-label={t('Open the menu')}>
              <PanelLeftOpen className="size-5" />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="right">{t('Open the menu ( [ )')}</TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button size="icon" className="mb-2" onClick={onAdd} aria-label={t('Add job')}>
              <Plus className="size-5" />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="right">{t('Add job')}</TooltipContent>
        </Tooltip>
        {item('today', 'Today')}
        <span className="my-1 h-px w-8 bg-border" />
        {STAGES.map(s => item(s.id, s.name))}
        <span className="flex-1" />
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="icon" onClick={onSettings} aria-label={t('Settings')}>
              <Gear className="size-5" />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="right">{t('Settings')}</TooltipContent>
        </Tooltip>
      </nav>
    </TooltipProvider>
  )
}
