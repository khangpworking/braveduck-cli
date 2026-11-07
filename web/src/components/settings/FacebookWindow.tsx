// The Facebook groups you follow: whether each one can be read, and
// Unfollow. Follow one by pasting its link in Add job.
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Q, S, type FBGroup } from '@/lib/settings'
import { N_ } from '@/lib/n'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { Body, CloseButton, Footer, Loading, Problem, Win, useErr, useFresh, type WinProps } from './parts'

const VISIBILITY: Record<FBGroup['visibility'], [string, string]> = {
  checking: [N_("Checking whether it's public…"), 'text-muted-foreground'],
  public: [N_('Public — its job posts come to your board'), 'text-emerald-700'],
  private: [N_('Private — only members see its posts. Paste the posts you want (link + text).'), 'text-amber-700'],
  unavailable: [N_("Can't be read right now"), 'text-red-700'],
}

/** Only http(s) links are links; anything else isn't clickable. */
const safeUrl = (u: string) => (/^https?:\/\//i.test(u) ? u : undefined)

export function FacebookWindow(p: WinProps) {
  const { t } = useTranslation()
  return (
    <Win {...p} title={t('Facebook groups you follow')} intro={t('To follow a group, paste its link in Add job.')}>
      <FacebookBody />
      <Footer>
        <CloseButton />
      </Footer>
    </Win>
  )
}

function FacebookBody() {
  const { t } = useTranslation()
  const err = useErr()
  const qc = useQueryClient()
  const q = useFresh(Q.fbGroups, S.fbGroups)
  const unfollow = useMutation({
    mutationFn: S.unfollow,
    onSuccess: () => void qc.invalidateQueries({ queryKey: Q.fbGroups }),
    onError: e => toast.error(err(e)),
  })
  if (q.isError) return <Body><Problem>{err(q.error)}</Problem></Body>
  if (!q.ready) return <Body><Loading /></Body>
  if (!q.data.length) return <Body><p className="text-muted-foreground">{t("You don't follow any Facebook groups yet.")}</p></Body>
  return (
    <Body>
      <ul>
        {q.data.map(g => {
          const [label, tone] = VISIBILITY[g.visibility] ?? [g.visibility, 'text-muted-foreground']
          return (
            <li key={g.key} className="flex items-center justify-between gap-3 border-b py-3 last:border-b-0" data-group={g.key}>
              <span className="min-w-0 [overflow-wrap:anywhere]">
                <a href={safeUrl(g.url)} target="_blank" rel="noopener noreferrer" className="font-semibold underline-offset-4 hover:underline">
                  {g.name || g.url}
                </a>
                <span className={cn('block text-[0.95rem]', tone)}>
                  {t(label)}
                  {g.note ? ` — ${g.note}` : ''}
                </span>
              </span>
              <Button variant="outline" className="h-11 shrink-0 text-base" disabled={unfollow.isPending} onClick={() => unfollow.mutate(g.key)}>
                {t('Unfollow')}
              </Button>
            </li>
          )
        })}
      </ul>
    </Body>
  )
}
