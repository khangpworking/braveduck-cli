// Pieces every settings window uses: the window itself, labelled fields,
// tags, choices, money, and a plain select.
import { Fragment, useId, useState } from 'react'
import { useQuery, type UseQueryResult } from '@tanstack/react-query'
import { Plus, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { locale } from '@/lib/i18n'
import { N_ } from '@/lib/n'
import { formatAmount, withAmount, type Money } from '@/lib/money'
import { addTags, unpicked } from '@/lib/tags'
import type { Msg } from '@/lib/schedule'
import { cn } from '@/lib/utils'

export interface WinProps {
  open: boolean
  onOpenChange: (o: boolean) => void
}

/**
 * A settings window: title, a short line under it, then its own Body (which
 * scrolls) and Footer (buttons). Its content only exists while it's open,
 * so each opening starts from what's saved.
 */
export function Win({ open, onOpenChange, title, intro, wide, children, ...drop }: WinProps & {
  title: string
  intro?: React.ReactNode
  wide?: boolean
  children: React.ReactNode
} & Pick<React.ComponentProps<'div'>, 'onDragOver' | 'onDrop' | 'onDragLeave'>) {
  const { t } = useTranslation()
  // A window opened again while it's still fading out starts afresh too.
  const [opening, setOpening] = useState(0)
  const [wasOpen, setWasOpen] = useState(open)
  if (open !== wasOpen) {
    setWasOpen(open)
    if (open) setOpening(n => n + 1)
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        showCloseButton={false}
        // On a touch screen a focused field pops the keyboard over the window.
        onOpenAutoFocus={e => matchMedia('(pointer: coarse)').matches && e.preventDefault()}
        {...drop}
        layout="panel"
        className={wide ? 'sm:max-w-2xl' : 'sm:max-w-lg'}
      >
        <DialogHeader bar>
          <DialogTitle>{title}</DialogTitle>
          {intro ? (
            <DialogDescription>{intro}</DialogDescription>
          ) : (
            <DialogDescription className="sr-only">{title}</DialogDescription>
          )}
        </DialogHeader>
        <Fragment key={opening}>{children}</Fragment>
        {/* Last, so the cursor starts in the window's first field. */}
        <DialogClose asChild>
          <Button variant="ghost" size="icon" className="absolute top-3 right-3 size-10" aria-label={t('Close')}>
            <X className="size-5" />
          </Button>
        </DialogClose>
      </DialogContent>
    </Dialog>
  )
}

export const Body = ({ children }: { children: React.ReactNode }) => (
  <div className="min-h-0 flex-1 space-y-5 overflow-y-auto px-5 py-4">{children}</div>
)

export const Footer = ({ children, className }: { children: React.ReactNode; className?: string }) => (
  // One row even on a phone, buttons sharing it, so the window keeps its room.
  <DialogFooter bar className={className}>{children}</DialogFooter>
)

/** The window's Close button, for a footer. */
export function CloseButton() {
  const { t } = useTranslation()
  return (
    <DialogClose asChild>
      <Button variant="outline" className="h-11">
        {t('Close')}
      </Button>
    </DialogClose>
  )
}

/**
 * What's saved, fetched again each time a window opens (live updates keep
 * the cache fresh, but a form must not start from an old copy). ready is
 * true once this opening's answer is in.
 */
export function useFresh<T>(queryKey: readonly unknown[], queryFn: () => Promise<T>): Fresh<T> {
  const q = useQuery({ queryKey, queryFn, refetchOnMount: 'always' })
  const ready = q.isSuccess && q.isFetchedAfterMount
  return ready ? { ...q, ready, data: q.data } : { ...q, ready: false }
}

type Fresh<T> = Omit<UseQueryResult<T>, 'data'> & ({ ready: true; data: T } | { ready: false; data?: T })

/** A [text, values] pair in the board's language, numbers written its way. */
export function useMsg() {
  const { t } = useTranslation()
  return ([k, v]: Msg) =>
    t(k, Object.fromEntries(Object.entries(v ?? {}).map(([n, x]) => [n, typeof x === 'number' ? x.toLocaleString(locale()) : x])))
}

/** Loading rows while a window's data arrives. */
export function Loading() {
  return (
    <div className="space-y-3" aria-hidden>
      <Skeleton className="h-6 w-2/3" />
      <Skeleton className="h-11 w-full" />
      <Skeleton className="h-11 w-full" />
    </div>
  )
}

/** A problem loading or saving, in the window. */
export function Problem({ children }: { children: React.ReactNode }) {
  return (
    <p role="alert" className="rounded-lg border border-danger-border bg-danger-muted px-3 py-2 text-danger">
      {children}
    </p>
  )
}

export function Field({ label, hint, htmlFor, children }: {
  label: string
  hint?: React.ReactNode
  htmlFor?: string
  children: React.ReactNode
}) {
  return (
    <div className="space-y-1.5">
      <label htmlFor={htmlFor} className="block font-semibold">
        {label}
      </label>
      {hint && <p className="text-small text-muted-foreground">{hint}</p>}
      {children}
    </div>
  )
}

export function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="space-y-4">
      <h3 className="text-lg font-semibold">{title}</h3>
      {children}
    </section>
  )
}

/**
 * A list the user types into: Enter or a comma adds, a paste splits on
 * commas, semicolons and lines, Backspace on an empty box removes the last,
 * and leaving the box keeps what was typed. Suggestions are tapped to add;
 * a suggestion we offered shows translated, what the user typed doesn't.
 */
export function TagsField({ label, hint, value, onChange, suggestions = [] }: {
  label: string
  hint?: React.ReactNode
  value: string[]
  onChange: (v: string[]) => void
  suggestions?: string[]
}) {
  const { t } = useTranslation()
  const id = useId()
  const [text, setText] = useState('')
  const shown = (v: string) => (suggestions.includes(v) ? t(v) : v)
  const commit = (typed = text) => {
    if (typed.trim()) onChange(addTags(value, typed))
    setText('')
  }
  return (
    <Field label={label} hint={hint} htmlFor={id}>
      <div className="flex flex-wrap items-center gap-1.5 rounded-lg border border-input p-1.5 focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50">
        {value.map((v, i) => (
          <span key={v} className="inline-flex h-9 items-center gap-1 rounded-md bg-secondary pl-3 text-body">
            {shown(v)}
            <button
              type="button"
              className="flex size-9 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground"
              aria-label={t('Remove {term}', { term: shown(v) })}
              onClick={() => onChange(value.filter((_, j) => j !== i))}
            >
              <X className="size-4" />
            </button>
          </span>
        ))}
        <input
          id={id}
          value={text}
          data-tags-input
          placeholder={t('Type and press Enter')}
          className="h-9 min-w-40 flex-1 bg-transparent px-2 text-base outline-none placeholder:text-muted-foreground"
          onChange={e => {
            const v = e.target.value
            if (/[,;\n]/.test(v)) commit(v)
            else setText(v)
          }}
          onKeyDown={e => {
            if (e.key === 'Enter') {
              e.preventDefault()
              commit()
            } else if (e.key === 'Backspace' && !text && value.length) {
              onChange(value.slice(0, -1))
            }
          }}
          onPaste={e => {
            const p = e.clipboardData.getData('text')
            if (/[,;\n]/.test(p)) {
              e.preventDefault()
              commit(text + p)
            }
          }}
          onBlur={() => commit()}
        />
      </div>
      {unpicked(suggestions, value).length > 0 && (
        <div className="flex flex-wrap gap-2 pt-1">
          {unpicked(suggestions, value).map(s => (
            <Button key={s} type="button" variant="outline" size="sm" className="h-10" onClick={() => onChange([...value, s])}>
              <Plus />
              {t(s)}
            </Button>
          ))}
        </div>
      )}
    </Field>
  )
}

/** Buttons that each turn on or off (several may be on). */
export function ChoiceField({ label, value, options, onChange }: {
  label: string
  value: string[]
  options: string[]
  onChange: (v: string[]) => void
}) {
  const { t } = useTranslation()
  // A saved value the form doesn't offer still shows, as written.
  const all = [...options, ...value.filter(v => !options.includes(v))]
  return (
    <div className="space-y-1.5">
      <div className="font-semibold">{label}</div>
      <div role="group" aria-label={label} className="flex flex-wrap gap-2">
        {all.map(o => {
          const on = value.includes(o)
          return (
            <Button
              key={o}
              type="button"
              aria-pressed={on}
              variant={on ? 'default' : 'outline'}
              onClick={() => onChange(on ? value.filter(v => v !== o) : [...value, o])}
            >
              {options.includes(o) ? t(o) : o}
            </Button>
          )
        })}
      </div>
    </div>
  )
}

/** A native select, big enough to read and tap. */
export function Select({ className, ...props }: React.ComponentProps<'select'>) {
  return (
    <select
      className={cn(
        'h-11 rounded-lg border border-input bg-background px-3 text-base outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:opacity-50',
        className,
      )}
      {...props}
    />
  )
}

const PERIODS: Record<string, string> = { month: N_('per month'), year: N_('per year'), hour: N_('per hour') }

/** Amount (with thousands separators), currency, period, and gross or net. */
export function MoneyField({ label, value, onChange, currencies, periods }: {
  label: string
  value: Money
  onChange: (m: Money) => void
  currencies: string[]
  periods: string[]
}) {
  const { t } = useTranslation()
  const id = useId()
  return (
    <Field label={label} htmlFor={id}>
      <div className="flex flex-wrap gap-2">
        <Input
          id={id}
          inputMode="numeric"
          value={formatAmount(value.amount, locale())}
          placeholder={t('e.g. 20,000,000')}
          aria-label={t('{label} amount', { label })}
          className="h-11 w-48"
          onChange={e => onChange(withAmount(value, e.target.value))}
        />
        <Select aria-label={t('{label} currency', { label })} value={value.currency} onChange={e => onChange({ ...value, currency: e.target.value })}>
          {currencies.map(c => (
            <option key={c} value={c}>
              {c}
            </option>
          ))}
        </Select>
        <Select aria-label={t('{label} period', { label })} value={value.period} onChange={e => onChange({ ...value, period: e.target.value })}>
          {periods.map(p => (
            <option key={p} value={p}>
              {PERIODS[p] ? t(PERIODS[p]) : p}
            </option>
          ))}
        </Select>
        <Select aria-label={t('{label} gross or net', { label })} value={value.basis ?? ''} onChange={e => onChange({ ...value, basis: e.target.value })}>
          <option value="">{t('gross or net?')}</option>
          <option value="gross">{t('gross (before tax)')}</option>
          <option value="net">{t('net (take-home)')}</option>
        </Select>
      </div>
      {value.text && <p className="text-small text-muted-foreground">{t('You wrote: "{text}". Fill in an amount to replace it.', { text: value.text })}</p>}
    </Field>
  )
}

/** A server's error in the board's language (its English text is the key). */
export function useErr() {
  const { t } = useTranslation()
  return (e: unknown) => t(e instanceof Error ? e.message : String(e))
}

/**
 * Runs fn once the focused field has let go: a tag typed without Enter is
 * added when its box loses focus, and that has to land before saving.
 */
export function afterBlur(fn: () => void) {
  ;(document.activeElement as HTMLElement | null)?.blur?.()
  setTimeout(fn, 0)
}
