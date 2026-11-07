// How jobs get rated: one line, Jev's notice and opt-out where Jev rates,
// and the raters (for whoever runs their own AI) under Advanced.
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ChevronDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Q, S, openWindow, type Jev, type RatingConfig } from '@/lib/settings'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Input } from '@/components/ui/input'
import { Body, CloseButton, Field, Footer, Loading, Problem, Win, useErr, useFresh, type WinProps } from './parts'

export function RatingWindow(p: WinProps) {
  const { t } = useTranslation()
  return (
    <Win {...p} title={t('How jobs get rated')} intro={t('Each job gets a grade from A to F, based on your profile.')}>
      <RatingBody close={() => p.onOpenChange(false)} />
    </Win>
  )
}

function RatingBody({ close }: { close: () => void }) {
  const { t } = useTranslation()
  const err = useErr()
  const cfg = useFresh(Q.rating, S.rating)
  const jev = useFresh(Q.jev, S.jev)
  return (
    <>
      <Body>
        {jev.ready && <JevBlock jev={jev.data} />}
        {cfg.isError ? <Problem>{err(cfg.error)}</Problem> : cfg.ready ? <Raters saved={cfg.data} close={close} /> : <Loading />}
      </Body>
      <Footer>
        <CloseButton />
        <Button className="h-11 text-base" onClick={() => openWindow('profile')}>
          {t('Edit my profile')}
        </Button>
      </Footer>
    </>
  )
}

function JevBlock({ jev }: { jev: Jev }) {
  const { t } = useTranslation()
  const err = useErr()
  const qc = useQueryClient()
  const set = useMutation({
    mutationFn: S.setJev,
    onSuccess: j => qc.setQueryData(Q.jev, j),
    onError: e => toast.error(err(e)),
  })
  if (!jev.available && !jev.opt_out) return null
  const out = set.isPending ? set.variables : jev.opt_out
  return (
    <div className="space-y-2 rounded-lg border bg-muted/40 p-3">
      {!out && <p className="text-[0.95rem] text-muted-foreground">{t("Your profile and jobs are sent to TypeSafe's Jev to rate them.")}</p>}
      <label className="flex min-h-11 cursor-pointer items-center gap-3">
        <Checkbox className="size-5" checked={out} disabled={set.isPending} onCheckedChange={v => set.mutate(v === true)} />
        {t("Don't send my profile to Jev")}
      </label>
    </div>
  )
}

function Raters({ saved, close }: { saved: RatingConfig; close: () => void }) {
  const { t } = useTranslation()
  const err = useErr()
  const qc = useQueryClient()
  const [ab, setAB] = useState(saved.ab_mode)
  const [def, setDef] = useState(saved.default_model)
  const [rivals, setRivals] = useState(saved.models.filter(m => m !== saved.default_model).join(', '))
  const save = useMutation({
    mutationFn: () => {
      const d = def.trim()
      const others = rivals.split(',').map(s => s.trim()).filter(s => s && s !== d)
      return S.saveRating({ default_model: d, ab_mode: ab, models: [d, ...new Set(others)] })
    },
    onSuccess: c => {
      qc.setQueryData(Q.rating, c)
      void qc.invalidateQueries({ queryKey: ['jobs'] })
      toast.success(t('Rating config updated!'))
      close()
    },
  })
  return (
    <Collapsible>
      <CollapsibleTrigger asChild>
        <Button variant="outline" className="group h-11 w-full justify-between text-base">
          {t('Advanced')}
          <ChevronDown className="transition-transform group-data-[state=open]:rotate-180" />
        </Button>
      </CollapsibleTrigger>
      <CollapsibleContent className="space-y-4 pt-4" data-advanced>
        <label className="flex cursor-pointer items-start gap-3">
          <Checkbox className="mt-0.5 size-5" checked={ab} onCheckedChange={v => setAB(v === true)} />
          {t('Enable A/B Rating Mode (Score using multiple models simultaneously)')}
        </label>
        <p className="text-[0.92rem] text-muted-foreground">
          {t('Raters are whichever AI you connect (e.g. Claude Code over MCP). Use the model id it reports when it submits ratings.')}
        </p>
        <Field label={t('Default rater (owns the score badge)')} htmlFor="rater-default">
          <Input id="rater-default" value={def} onChange={e => setDef(e.target.value)} placeholder={t('e.g. claude-opus-5-5')} className="h-11 text-base md:text-base" />
        </Field>
        <Field label={t('Challenger raters (A/B mode, up to 2, comma-separated)')} htmlFor="rater-rivals">
          <Input id="rater-rivals" value={rivals} onChange={e => setRivals(e.target.value)} placeholder={t('e.g. gpt-sol-6-1, gemini-3-8')} className="h-11 text-base md:text-base" />
        </Field>
        {save.isError && <Problem>{err(save.error)}</Problem>}
        <Button variant="secondary" className="h-11 text-base" disabled={save.isPending} onClick={() => save.mutate()}>
          {save.isPending ? t('Saving…') : t('Save')}
        </Button>
      </CollapsibleContent>
    </Collapsible>
  )
}
