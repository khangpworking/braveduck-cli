// The folded parts of a job: notes, the CV and email for it, the posting
// itself, and what other AIs thought. Closed until wanted, so the job reads
// short.
import { useEffect, useRef, useState } from 'react'
import { ChevronRight, Download, Mail, Sparkles } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import type { Job } from '@/lib/api'
import { download, draftOf, getRatingConfig, mailto, type Draft } from '@/lib/jobApi'
import { locale } from '@/lib/i18n'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import type { JobActions } from './useJobActions'

export function Section({ title, hint, open, children }: {
  title: string
  hint?: string
  open?: boolean
  children: React.ReactNode
}) {
  return (
    <Collapsible defaultOpen={open} className="border-t">
      <CollapsibleTrigger asChild>
        <button type="button" className="group flex min-h-14 w-full items-center gap-3 text-left">
          <span className="flex-1">
            <span className="block text-[1.06rem] font-semibold">{title}</span>
            {hint && <span className="block text-sm text-muted-foreground">{hint}</span>}
          </span>
          <ChevronRight className="size-5 text-muted-foreground transition-transform group-data-[state=open]:rotate-90" />
        </button>
      </CollapsibleTrigger>
      <CollapsibleContent className="space-y-3 pb-4">{children}</CollapsibleContent>
    </Collapsible>
  )
}

/** Notes save themselves half a second after the last key. */
export function Notes({ job, a }: { job: Job; a: JobActions }) {
  const { t } = useTranslation()
  const [v, setV] = useState(job.notes)
  const timer = useRef(0)
  const typing = useRef(false)
  // A refetch must not overwrite what's being typed.
  useEffect(() => {
    if (!typing.current) setV(job.notes)
  }, [job.notes])
  useEffect(() => () => clearTimeout(timer.current), [])
  return (
    <Section title={t('My notes')} hint={job.notes ? undefined : t('Saved as you type')} open={!!job.notes}>
      <Textarea
        value={v}
        aria-label={t('My notes')}
        placeholder={t('Who you talked to, what to ask, anything to remember')}
        className="min-h-24 text-base"
        onChange={e => {
          const next = e.target.value
          setV(next)
          typing.current = true
          clearTimeout(timer.current)
          timer.current = window.setTimeout(() => {
            a.edit.mutate({ notes: next }, { onSettled: () => (typing.current = false) })
          }, 500)
        }}
      />
    </Section>
  )
}

function EmailDraft({ job, a, d }: { job: Job; a: JobActions; d: Draft }) {
  const { t } = useTranslation()
  const [v, setV] = useState(d)
  const [asking, setAsking] = useState(false)
  const [discard, setDiscard] = useState(false)
  // A new draft (from the AI) replaces what is shown; a refetch of the same one doesn't.
  useEffect(() => setV({ to: d.to, subject: d.subject, body: d.body }), [d.to, d.subject, d.body])
  const sent = job.draft_status === 'sent'
  const send = () =>
    a.draft.save.mutate(v, {
      onSuccess: () => {
        location.href = mailto(v)
        setTimeout(() => setAsking(true), 1500)
      },
    })
  return (
    <div className="space-y-2">
      <label className="grid gap-1 text-sm font-medium">
        {t('To')}
        <Input value={v.to} readOnly={sent} placeholder="hr@company.com" onChange={e => setV({ ...v, to: e.target.value })} className="h-11 text-base" />
      </label>
      <label className="grid gap-1 text-sm font-medium">
        {t('Subject')}
        <Input value={v.subject} readOnly={sent} onChange={e => setV({ ...v, subject: e.target.value })} className="h-11 text-base" />
      </label>
      <label className="grid gap-1 text-sm font-medium">
        {t('Email')}
        <Textarea value={v.body} readOnly={sent} onChange={e => setV({ ...v, body: e.target.value })} className="min-h-40 text-base" />
      </label>
      {sent ? (
        <p className="font-medium text-emerald-700">{t('✓ Sent (you said you sent it).')}</p>
      ) : (
        <>
          <p className="text-sm text-muted-foreground">{t('Attach the CV for this job in your mail app before you send it.')}</p>
          <div className="flex flex-wrap gap-2">
            <Button className="h-11 text-base" onClick={send}>
              <Mail />
              {t('Send from my mail app')}
            </Button>
            <Button variant="outline" className="h-11 text-base" onClick={() => a.askAI.mutate({ kind: 'draft', notes: job.draft_notes })}>
              {t('Write it again')}
            </Button>
            <Button variant="ghost" className="h-11 text-base text-muted-foreground" onClick={() => setDiscard(true)}>
              {t('Discard')}
            </Button>
          </div>
        </>
      )}
      <AlertDialog open={asking} onOpenChange={setAsking}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Did you send the email?')}</AlertDialogTitle>
            <AlertDialogDescription>{t('If you did, braveduck marks it sent and moves the job to Applied.')}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel className="h-11 text-base">{t('Not yet')}</AlertDialogCancel>
            <AlertDialogAction className="h-11 text-base" onClick={() => a.draft.sent.mutate()}>
              {t('Yes, I sent it')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog open={discard} onOpenChange={setDiscard}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Discard this email?')}</AlertDialogTitle>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel className="h-11 text-base">{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction variant="destructive" className="h-11 text-base" onClick={() => a.draft.reject.mutate()}>
              {t('Discard')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

/** The CV and the email for this job (before and right after applying). */
export function Documents({ job, a }: { job: Job; a: JobActions }) {
  const { t } = useTranslation()
  const [notes, setNotes] = useState(job.draft_notes)
  const d = draftOf(job)
  const cvReady = job.cv_status === 'ready'
  const hint = [cvReady ? t('CV ready') : '', d ? (job.draft_status === 'sent' ? t('email sent') : t('email ready')) : '']
    .filter(Boolean)
    .join(' · ')
  return (
    <Section title={t('CV and email for this job')} hint={hint || t('Have your AI write them')}>
      <div className="space-y-2 rounded-xl border p-4">
        <h3 className="font-semibold">{t('CV for this job')}</h3>
        <p className="text-muted-foreground">
          {cvReady
            ? t('Version {n} · {date}', {
                n: job.cv_version,
                date: new Date(job.cv_generated_at).toLocaleString(locale(), { dateStyle: 'short', timeStyle: 'short' }),
              })
            : t('Not written yet. Without one, your main CV is used.')}
        </p>
        <div className="flex flex-wrap gap-2">
          {cvReady && (
            <Button
              variant="outline"
              className="h-11 text-base"
              onClick={() => download(`/jobs/${job.id}/cv.pdf`, `cv-job-${job.id}.pdf`).catch((e: Error) => toast.error(t(e.message)))}
            >
              <Download />
              {t('Download the PDF')}
            </Button>
          )}
          <Button variant="outline" className="h-11 text-base" onClick={() => a.askAI.mutate({ kind: 'cv' })}>
            <Sparkles />
            {cvReady ? t('Ask your AI to improve it') : t('Ask your AI to tailor it')}
          </Button>
        </div>
      </div>
      <div className="space-y-2 rounded-xl border p-4">
        <h3 className="font-semibold">{t('Email to the company')}</h3>
        {d ? (
          <EmailDraft job={job} a={a} d={d} />
        ) : (
          <>
            <Textarea
              value={notes}
              onChange={e => setNotes(e.target.value)}
              aria-label={t('Notes for your AI')}
              placeholder={t('Anything your AI should know (tone, what to stress)… optional')}
              className="min-h-20 text-base"
            />
            <Button variant="outline" className="h-11 text-base" onClick={() => a.askAI.mutate({ kind: 'draft', notes })}>
              <Sparkles />
              {t('Ask your AI to write it')}
            </Button>
          </>
        )}
      </div>
    </Section>
  )
}

/** The posting's own words: the summary and the skills it asks for. */
export function AboutJob({ job }: { job: Job }) {
  const { t } = useTranslation()
  const points = job.jd_ai_summary.split('\n').map(s => s.replace(/^\s*[-•*]\s*/, '').trim()).filter(Boolean)
  const skills = job.skills.split(',').map(s => s.trim()).filter(Boolean)
  if (!points.length && !job.jd_summary && !skills.length) return null
  return (
    <Section title={t('About the job')}>
      {points.length ? (
        <ul className="list-disc space-y-1 pl-5 text-[1.02rem]">
          {points.map(p => (
            <li key={p}>{p}</li>
          ))}
        </ul>
      ) : (
        <p className="text-[1.02rem] whitespace-pre-line">{job.jd_summary}</p>
      )}
      {skills.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {skills.map(s => (
            <span key={s} className="rounded-full bg-muted px-3 py-1 text-sm">
              {s}
            </span>
          ))}
        </div>
      )}
    </Section>
  )
}

/** With A/B rating on: the challengers' verdicts, and asking one that hasn't rated. */
export function OtherRatings({ job, a }: { job: Job; a: JobActions }) {
  const { t } = useTranslation()
  const cfg = useQuery({ queryKey: ['rating-config'], queryFn: getRatingConfig })
  if (!cfg.data?.ab_mode) return null
  const others = cfg.data.models.filter(m => m !== cfg.data.default_model)
  const rated = others.filter(m => job.ratings?.[m] && job.ratings[m].letter && job.ratings[m].letter !== '...' && job.ratings[m].letter !== '✕')
  const missing = others.filter(m => !rated.includes(m))
  return (
    <Section title={t('Other AI ratings')} hint={rated.length ? undefined : t('No other AI has rated this job yet.')}>
      {rated.map(m => (
        <div key={m} className="rounded-lg border p-3">
          <b>
            {m}: {job.ratings![m].letter} {job.ratings![m].score.toLocaleString(locale())}
          </b>
          <p className="text-muted-foreground">{job.ratings![m].reasoning}</p>
        </div>
      ))}
      <div className="flex flex-wrap gap-2">
        {missing.map(m => (
          <Button key={m} variant="outline" className="h-10" onClick={() => a.askAI.mutate({ kind: 'rate', model: m })}>
            {t('Ask {model}', { model: m })}
          </Button>
        ))}
      </div>
    </Section>
  )
}
