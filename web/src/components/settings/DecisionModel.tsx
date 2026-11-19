// Which decision model rates jobs (Jev or Clef) and the keys for each. Keys
// are write-only: the board only learns whether one is saved, never the key.
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Check } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Q, S, type Decision, type DecisionUpdate } from '@/lib/settings'
import type { KeyState } from '@/lib/gen/service'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { Field, Problem, useErr } from './parts'

export function DecisionModel({ saved }: { saved: Decision }) {
  const { t } = useTranslation()
  const err = useErr()
  const qc = useQueryClient()
  const [jevKey, setJevKey] = useState('')
  const [clefToken, setClefToken] = useState('')
  const [account, setAccount] = useState(saved.clef_account_id ?? '')
  const save = useMutation({
    mutationFn: S.saveDecision,
    onSuccess: (d, u) => {
      qc.setQueryData(Q.decision, d)
      void qc.invalidateQueries({ queryKey: Q.jev })
      void qc.invalidateQueries({ queryKey: Q.rating })
      void qc.invalidateQueries({ queryKey: ['jobs'] })
      if (u.jev_key !== undefined) setJevKey('')
      if (u.clef_token !== undefined) setClefToken('')
      if (u.jev_key !== undefined || u.clef_token !== undefined || u.clef_account_id !== undefined) toast.success(t('Saved'))
    },
  })
  const send = (u: DecisionUpdate) => save.mutate(u)
  const provider = save.isPending && save.variables.provider ? save.variables.provider : saved.provider
  const clefModel = save.isPending && save.variables.clef_model ? save.variables.clef_model : saved.clef_model
  const on = (yes: boolean) => (yes ? <Check data-icon="inline-start" aria-hidden /> : null)
  const status = (k: KeyState) =>
    k.saved ? t('A key is saved.') : k.source ? t('Using the key from {source}.', { source: k.source }) : t('No key yet.')

  return (
    <div className="space-y-4 rounded-lg border p-3">
      <Field label={t('Rating model')} hint={t('Jev and Clef answer the same questions about each job. Pick one; you can switch any time.')}>
        <ToggleGroup
          type="single"
          variant="outline"
          size="xl"
          aria-label={t('Rating model')}
          value={provider}
          onValueChange={v => {
            if ((v === 'jev' || v === 'clef') && v !== saved.provider) send({ provider: v })
          }}
        >
          <ToggleGroupItem value="jev">
            {on(provider === 'jev')}
            {t('Jev (TypeSafe)')}
          </ToggleGroupItem>
          <ToggleGroupItem value="clef">
            {on(provider === 'clef')}
            {t('Clef (Cloudflare)')}
          </ToggleGroupItem>
        </ToggleGroup>
      </Field>

      <Field label={t('Jev API key')} hint={status(saved.jev)} htmlFor="jev-key">
        <Input
          id="jev-key"
          type="password"
          autoComplete="off"
          value={jevKey}
          onChange={e => setJevKey(e.target.value)}
          placeholder={t('Paste a TypeSafe API key')}
          className="h-11"
        />
      </Field>
      <div className="flex flex-wrap gap-2">
        <Button variant="secondary" className="h-11" disabled={save.isPending || !jevKey.trim()} onClick={() => send({ jev_key: jevKey.trim() })}>
          {t('Save Jev key')}
        </Button>
        {saved.jev.saved && (
          <Button variant="outline" className="h-11" disabled={save.isPending} onClick={() => send({ jev_key: '' })}>
            {t('Remove Jev key')}
          </Button>
        )}
      </div>

      <Field label={t('Cloudflare account ID')} htmlFor="clef-account">
        <Input
          id="clef-account"
          value={account}
          onChange={e => setAccount(e.target.value)}
          placeholder={t('32 letters and digits, from the Cloudflare dashboard')}
          className="h-11"
        />
      </Field>
      <Field label={t('Workers AI token')} hint={status(saved.clef)} htmlFor="clef-token">
        <Input
          id="clef-token"
          type="password"
          autoComplete="off"
          value={clefToken}
          onChange={e => setClefToken(e.target.value)}
          placeholder={t('Paste a Cloudflare API token with Workers AI permission')}
          className="h-11"
        />
      </Field>
      <Field label={t('Clef model')}>
        <ToggleGroup
          type="single"
          variant="outline"
          size="xl"
          aria-label={t('Clef model')}
          value={clefModel}
          onValueChange={v => {
            if ((v === 'clef-flash' || v === 'clef') && v !== saved.clef_model) send({ clef_model: v })
          }}
        >
          <ToggleGroupItem value="clef-flash">
            {on(clefModel === 'clef-flash')}
            {t('Fast (clef-flash)')}
          </ToggleGroupItem>
          <ToggleGroupItem value="clef">
            {on(clefModel === 'clef')}
            {t('Careful (clef)')}
          </ToggleGroupItem>
        </ToggleGroup>
      </Field>
      <div className="flex flex-wrap gap-2">
        <Button
          variant="secondary"
          className="h-11"
          disabled={save.isPending || (!clefToken.trim() && account.trim() === (saved.clef_account_id ?? ''))}
          onClick={() => send({ clef_account_id: account.trim(), ...(clefToken.trim() ? { clef_token: clefToken.trim() } : {}) })}
        >
          {t('Save Clef settings')}
        </Button>
        {saved.clef.saved && (
          <Button variant="outline" className="h-11" disabled={save.isPending} onClick={() => send({ clef_token: '' })}>
            {t('Remove Clef token')}
          </Button>
        )}
      </div>
      {save.isError && <Problem>{err(save.error)}</Problem>}
    </div>
  )
}
