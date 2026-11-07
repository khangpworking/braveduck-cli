// Your profile: what every job is rated against. A short form (tags,
// choices, pay) by default; the whole Markdown file for whoever wants it.
// The server writes only the lines the form owns, and every field the form
// doesn't show goes back unchanged.
import { useRef, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ChevronDown, CircleCheck, MapPin } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Q, S, openWindow, type Profile, type ProfileForm, type ProfileOptions } from '@/lib/settings'
import { withDefaults } from '@/lib/money'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Textarea } from '@/components/ui/textarea'
import { Body, ChoiceField, Field, Footer, Loading, MoneyField, Problem, Section, TagsField, Win, afterBlur, useErr, useFresh, type WinProps } from './parts'

export function ProfileWindow(p: WinProps) {
  const { t } = useTranslation()
  return (
    <Win {...p} wide title={t('Your profile')} intro={t("Every job is rated against this. Fill in what applies to you, in any language — skip anything you're unsure about.")}>
      <ProfileBody close={() => p.onOpenChange(false)} />
    </Win>
  )
}

function ProfileBody({ close }: { close: () => void }) {
  const { t } = useTranslation()
  const err = useErr()
  const form = useFresh(Q.profileForm, S.profileForm)
  const prof = useFresh(Q.profile, S.profile)
  const failed = form.error ?? prof.error
  if (failed) return <Body><Problem>{t("Couldn't load your profile: {error}", { error: err(failed) })}</Problem></Body>
  if (!form.ready || !prof.ready) return <Body><Loading /></Body>
  return <ProfileEditor saved={form.data} profile={prof.data} close={close} />
}

type Mode = 'form' | 'text'

function ProfileEditor({ saved, profile, close }: {
  saved: { form: ProfileForm; options: ProfileOptions }
  profile: Profile
  close: () => void
}) {
  const { t } = useTranslation()
  const err = useErr()
  const qc = useQueryClient()
  const o = saved.options
  const [mode, setMode] = useState<Mode>('form')
  const [form, setForm] = useState<ProfileForm>(() => ({ ...saved.form, min_pay: withDefaults(saved.form.min_pay) }))
  const [text, setText] = useState(profile.text)
  const [filled, setFilled] = useState(profile.filled)
  const latest = useRef({ form, text, mode })
  latest.current = { form, text, mode }
  const set =
    <K extends keyof ProfileForm>(k: K) =>
    (v: ProfileForm[K]) =>
      setForm(f => ({ ...f, [k]: v }))

  // Saves whichever editor is showing, then reads the profile back.
  const save = useMutation({
    mutationFn: async () => {
      const { form, text, mode } = latest.current
      if (mode === 'form') {
        const r = await S.saveProfileForm(form)
        setForm({ ...r.form, min_pay: withDefaults(r.form.min_pay) })
      } else {
        await S.saveProfile(text)
      }
      const p = await S.profile()
      setText(p.text)
      setFilled(p.filled)
    },
    onSuccess: () => {
      for (const k of [Q.profile, Q.profileForm, Q.setup, Q.cv]) void qc.invalidateQueries({ queryKey: k })
    },
    onError: e => toast.error(t("Couldn't save: {error}", { error: err(e) })),
  })

  const switchMode = () =>
    afterBlur(() =>
      save.mutate(undefined, {
        onSuccess: async () => {
          if (latest.current.mode === 'text') {
            const r = await S.profileForm()
            setForm({ ...r.form, min_pay: withDefaults(r.form.min_pay) })
          }
          setMode(m => (m === 'form' ? 'text' : 'form'))
        },
      }),
    )

  return (
    <>
      <Body>
        <p className="flex items-start gap-2" role="status">
          {filled ? (
            <>
              <CircleCheck className="mt-0.5 size-5 shrink-0 text-success" />
              {t('Filled in — jobs can be rated against it.')}
            </>
          ) : (
            <span className="font-medium text-warning-strong">{t("Not filled in yet, so jobs can't be rated. Start with the job titles you want.")}</span>
          )}
        </p>
        {mode === 'text' ? (
          <Field label={t('Profile (Markdown)')} hint={t('The whole profile as text (Markdown). Lines inside <!-- --> are hints and are ignored.')} htmlFor="profile-text">
            <Textarea id="profile-text" spellCheck={false} value={text} onChange={e => setText(e.target.value)} code className="min-h-96" />
          </Field>
        ) : (
          <>
            <Section title={t('1. What job do you want?')}>
              <TagsField label={t('Job titles')} hint={t("The roles you'd apply for, e.g. Business Analyst, Kế toán.")} value={form.roles} onChange={set('roles')} />
              <ChoiceField label={t('Level')} value={form.seniority} options={o.seniority} onChange={set('seniority')} />
              <ChoiceField label={t('Type of job')} value={form.job_types} options={o.job_types} onChange={set('job_types')} />
              <ChoiceField label={t('Work mode')} value={form.work_modes} options={o.work_modes} onChange={set('work_modes')} />
              <p className="flex flex-wrap items-center gap-x-1 text-small text-muted-foreground">
                <MapPin className="size-4" />
                {t("Where you live and how far you'll travel to work:")}
                <Button variant="link" size="inline" onClick={() => openWindow('location')}>
                  {t('set your location →')}
                </Button>
              </p>
            </Section>
            <Section title={t('2. Pay')}>
              <MoneyField label={t("The least you'd accept")} value={form.min_pay} onChange={set('min_pay')} currencies={o.currencies} periods={o.periods} />
            </Section>
            <Section title={t('3. Deal-breakers')}>
              <TagsField
                label={t('A job is a "no" if…')}
                hint={t('Saturday work and unstated pay are also flagged on job cards. Anything you add here guides the rating.')}
                value={form.deal_breakers}
                suggestions={o.deal_breakers}
                onChange={set('deal_breakers')}
              />
            </Section>
            <Section title={t('4. About you')}>
              <Field label={t('Your experience and skills')} hint={t('Filled in from your CV when you upload one. Edit freely.')} htmlFor="profile-about">
                <Textarea id="profile-about" rows={6} value={form.about_me} onChange={e => set('about_me')(e.target.value)} className="min-h-36" />
              </Field>
            </Section>
            <Collapsible>
              <CollapsibleTrigger asChild>
                <Button variant="outline" className="group h-11 w-full justify-between">
                  {t('More (optional)')}
                  <ChevronDown className="transition-transform group-data-[state=open]:rotate-180" />
                </Button>
              </CollapsibleTrigger>
              <CollapsibleContent>
<div className="space-y-5 pt-4">
                <TagsField label={t('Industries you like')} value={form.industries_want} onChange={set('industries_want')} />
                <TagsField label={t('Industries to avoid')} value={form.industries_avoid} onChange={set('industries_avoid')} />
                <TagsField label={t('Languages you can work in')} value={form.languages} suggestions={o.languages} onChange={set('languages')} />
                <Field label={t('Anything else the rater should know')} htmlFor="profile-notes">
                  <Textarea id="profile-notes" rows={3} value={form.notes} onChange={e => set('notes')(e.target.value)} />
                </Field>
              </div>
</CollapsibleContent>
            </Collapsible>
          </>
        )}
        <Button variant="link" size="inline" className="h-11" disabled={save.isPending} onClick={switchMode}>
          {mode === 'form' ? t('Edit as text instead') : t('Back to the form')}
        </Button>
      </Body>
      <Footer>
        <Button variant="outline" className="h-11" onClick={close}>
          {t('Close')}
        </Button>
        <Button
          className="h-11"
          disabled={save.isPending}
          onClick={() => afterBlur(() => save.mutate(undefined, { onSuccess: () => toast.success(t('Profile saved')) }))}
        >
          {save.isPending ? t('Saving…') : t('Save')}
        </Button>
      </Footer>
    </>
  )
}
