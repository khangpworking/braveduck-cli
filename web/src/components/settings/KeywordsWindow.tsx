// Search terms: one question as tags with a live count; the filter lists
// wait under More filters. Every list goes back to the server on Save.
import { useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { getJobs } from '@/lib/api'
import { FILTERS, Q, S, type Keywords } from '@/lib/settings'
import { mentionCount } from '@/lib/tags'
import { locale } from '@/lib/i18n'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Body, Footer, Loading, Problem, TagsField, Win, afterBlur, useErr, useFresh, type WinProps } from './parts'

export function KeywordsWindow(p: WinProps) {
  const { t } = useTranslation()
  return (
    <Win {...p} title={t('Search terms')}>
      <KeywordsBody close={() => p.onOpenChange(false)} />
    </Win>
  )
}

function KeywordsBody({ close }: { close: () => void }) {
  const { t } = useTranslation()
  const err = useErr()
  const q = useFresh(Q.keywords, S.keywords)
  if (q.isError) return <Body><Problem>{t("Couldn't load your search terms: {error}", { error: err(q.error) })}</Problem></Body>
  if (!q.ready) return <Body><Loading /></Body>
  return <KeywordsForm saved={q.data} close={close} />
}

function KeywordsForm({ saved, close }: { saved: Keywords; close: () => void }) {
  const { t } = useTranslation()
  const err = useErr()
  const qc = useQueryClient()
  const jobs = useQuery({ queryKey: ['jobs'], queryFn: getJobs }).data ?? []
  const [lists, setLists] = useState<Keywords>(() => {
    const out: Keywords = { ...saved, search_queries: saved.search_queries ?? [] }
    for (const [key] of FILTERS) out[key] = saved[key] ?? []
    return out
  })
  const latest = useRef(lists)
  latest.current = lists
  const set = (key: string) => (v: string[]) => setLists(l => ({ ...l, [key]: v }))
  const inUse = FILTERS.reduce((n, [key]) => n + (lists[key]?.length ?? 0), 0)
  const n = mentionCount(jobs, lists.search_queries)

  const save = useMutation({
    mutationFn: S.saveKeywords,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: Q.keywords })
      void qc.invalidateQueries({ queryKey: Q.setup })
      toast.success(t('Search terms saved.'))
      close()
    },
    onError: e => toast.error(t("Couldn't save: {error}", { error: err(e) })),
  })

  return (
    <>
      <Body>
        <TagsField
          label={t('What jobs should we look for?')}
          hint={t('Job titles or skills, e.g. Kế toán, n8n.')}
          value={lists.search_queries}
          onChange={set('search_queries')}
        />
        <p className="-mt-2 text-small text-muted-foreground" role="status" data-count>
          {!lists.search_queries.length
            ? ''
            : n === 0
              ? t('No jobs on your board mention these yet.')
              : n === 1
                ? t('1 job on your board mentions these.')
                : t('{n} jobs on your board mention these.', { n: n.toLocaleString(locale()) })}
        </p>
        <Collapsible>
          <CollapsibleTrigger asChild>
            <Button variant="outline" className="group h-11 w-full justify-between">
              {inUse ? t('More filters ({n} in use)', { n: inUse }) : t('More filters')}
              <ChevronDown className="transition-transform group-data-[state=open]:rotate-180" />
            </Button>
          </CollapsibleTrigger>
          <CollapsibleContent>
<div className="space-y-5 pt-4">
            {FILTERS.map(([key, label, hint]) => (
              <TagsField key={key} label={t(label)} hint={hint && t(hint)} value={lists[key]} onChange={set(key)} />
            ))}
          </div>
</CollapsibleContent>
        </Collapsible>
      </Body>
      <Footer>
        <Button variant="outline" className="h-11" onClick={close}>
          {t('Cancel')}
        </Button>
        <Button className="h-11" disabled={save.isPending} onClick={() => afterBlur(() => save.mutate(latest.current))}>
          {save.isPending ? t('Saving…') : t('Save')}
        </Button>
      </Footer>
    </>
  )
}
