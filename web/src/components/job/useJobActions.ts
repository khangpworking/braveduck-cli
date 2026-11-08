// Every action on one job, with its toast. Writes go through the API and the
// board refetches (live updates bring the same change to other tabs).
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { ApiError, jobChanged, type Job, type JobPatch } from '@/lib/api'
import * as jobApi from '@/lib/jobApi'
import { N_ } from '@/lib/n'
import type { AskKind } from './AskAI'

export function useJobActions(job: Job) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const done = () => jobChanged(qc, job.id)
  const fail = (what: string) => (e: Error) => toast.error(t(what, { error: t(e.message) }))
  const [ask, setAsk] = useState<{ kind: AskKind; ask: jobApi.Ask } | null>(null)
  const [prep, setPrep] = useState<jobApi.ApplyPrep | null>(null)

  const edit = useMutation({
    mutationFn: (p: JobPatch) => jobApi.edit(job.id, p),
    onSuccess: done,
    onError: fail(N_("Couldn't save: {error}")),
  })
  const pin = useMutation({
    mutationFn: () => jobApi.togglePin(job.id),
    onSuccess: j => {
      done()
      toast(j.pinned ? t('Pinned to the top') : t('No longer pinned'))
    },
    onError: fail(N_("Couldn't save: {error}")),
  })
  const keep = useMutation({
    mutationFn: () => jobApi.toggleKeep(job.id),
    onSuccess: j => {
      done()
      toast(j.keep ? t('Kept: it won’t move to Closed by itself') : t('No longer kept'))
    },
    onError: fail(N_("Couldn't save: {error}")),
  })
  const remove = useMutation({
    mutationFn: () => jobApi.remove(job.id),
    onSuccess: () => {
      done()
      toast(t('Job deleted'))
    },
    onError: fail(N_("Couldn't delete: {error}")),
  })
  const followedUp = useMutation({
    mutationFn: () => jobApi.followedUp(job.id),
    onSuccess: () => {
      done()
      toast(t('Noted. If there’s still no answer in a week, braveduck reminds you again.'))
    },
    onError: fail(N_("Couldn't save: {error}")),
  })
  const apply = useMutation({
    mutationFn: () => jobApi.startApply(job.id),
    onSuccess: p => {
      done()
      setPrep(p)
      if (p.cover_letter) void navigator.clipboard.writeText(p.cover_letter).catch(() => {})
    },
    onError: fail(N_("Couldn't open the site: {error}")),
  })
  const confirm = useMutation({
    mutationFn: (submitted: boolean) => jobApi.confirmApplied(job.id, submitted),
    onSuccess: (_, submitted) => {
      done()
      if (submitted) toast(t('Moved to Applied'))
    },
    onError: fail(N_("Couldn't save: {error}")),
  })
  const askAI = useMutation({
    mutationFn: async ({ kind, model, notes }: { kind: AskKind; model?: string; notes?: string }) => {
      if (kind === 'rate') return { kind, res: await jobApi.askRating(job.id, model) }
      if (kind === 'cv') return { kind, res: await jobApi.askCV(job.id) }
      return { kind, res: await jobApi.askDraft(job.id, notes ?? '') }
    },
    onMutate: ({ kind }) => kind === 'rate' && toast(t('Rating…')),
    onSuccess: ({ kind, res }) => {
      if ('rated' in res && res.rated) {
        done()
        const r = res.job?.ratings?.[res.model]
        const letter = r?.letter ?? res.job?.score_letter ?? ''
        toast(t('Rated {letter}', { letter }))
        return
      }
      setAsk({ kind, ask: { prompt: res.prompt ?? '', brief: res.brief ?? '' } })
    },
    onError: (e: Error) => {
      if (e instanceof ApiError && e.body.code === 'profile_empty') {
        toast(t('Fill in your profile first, so there’s something to rate jobs against.'))
        window.dispatchEvent(new CustomEvent('braveduck:open', { detail: 'profile' }))
        return
      }
      toast.error(t(e.message))
    },
  })
  const draft = {
    save: useMutation({
      mutationFn: (d: jobApi.Draft) => jobApi.saveDraft(job.id, d),
      onSuccess: done,
      onError: fail(N_("Couldn't save: {error}")),
    }),
    sent: useMutation({
      mutationFn: () => jobApi.draftSent(job.id),
      onSuccess: () => {
        done()
        toast(t('Marked as sent.'))
      },
      onError: fail(N_("Couldn't save: {error}")),
    }),
    reject: useMutation({
      mutationFn: () => jobApi.draftReject(job.id),
      onSuccess: done,
      onError: fail(N_("Couldn't save: {error}")),
    }),
  }
  return { edit, pin, keep, remove, followedUp, apply, confirm, askAI, draft, ask, setAsk, prep, setPrep }
}

export type JobActions = ReturnType<typeof useJobActions>
