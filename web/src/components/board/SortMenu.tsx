// How the list is ordered, chosen per stage and remembered.
import { useEffect, useState } from 'react'
import { ArrowDownUp } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { SORTS, type SortKey } from '@/lib/jobs'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuLabel, DropdownMenuRadioGroup, DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

const key = (view: string) => `braveduck.sort.${view}`

/** The order chosen for view (a stage), saved across visits; best fit at first. */
export function useSort(view: string) {
  const read = () => {
    try {
      const v = localStorage.getItem(key(view))
      return SORTS.some(s => s.id === v) ? (v as SortKey) : 'fit'
    } catch {
      return 'fit'
    }
  }
  const [sort, setSort] = useState<SortKey>(read)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useEffect(() => setSort(read()), [view])
  const choose = (s: SortKey) => {
    setSort(s)
    try {
      localStorage.setItem(key(view), s)
    } catch {
      // nothing to save to
    }
  }
  return [sort, choose] as const
}

export function SortMenu({ sort, onSort }: { sort: SortKey; onSort: (s: SortKey) => void }) {
  const { t } = useTranslation()
  const name = SORTS.find(s => s.id === sort)!.name
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" aria-label={t('Sort: {order}', { order: t(name) })}>
          <ArrowDownUp />
          {t(name)}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel>{t('Sort by')}</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={sort} onValueChange={v => onSort(v as SortKey)}>
          {SORTS.map(s => (
            <DropdownMenuRadioItem key={s.id} value={s.id}>
              {t(s.name)}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
