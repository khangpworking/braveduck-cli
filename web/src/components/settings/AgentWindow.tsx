// Connect your AI agent (shared server only): the agent reaches this
// board's MCP over HTTPS with a key made here, shown once.
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Copy } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Q, S } from '@/lib/settings'
import { Button } from '@/components/ui/button'
import { Body, CloseButton, Footer, Loading, Problem, Win, useErr, useFresh, type WinProps } from './parts'

/** The command that connects Claude Code to this board with key. */
export const connectCommand = (origin: string, key: string) =>
  `claude mcp add --transport http braveduck ${origin}/mcp \\\n  --header "Authorization: Bearer ${key}"`

export function AgentWindow(p: WinProps) {
  const { t } = useTranslation()
  return (
    <Win
      {...p}
      title={t('Connect your AI agent')}
      intro={t('Your AI agent (e.g. Claude Code) can then rate jobs, tailor your CV and draft emails on this board. It uses a key made here — never your password.')}
    >
      <AgentBody />
      <Footer>
        <CloseButton />
      </Footer>
    </Win>
  )
}

function AgentBody() {
  const { t } = useTranslation()
  const err = useErr()
  const qc = useQueryClient()
  const q = useFresh(Q.agentKey, S.agentKey)
  const [cmd, setCmd] = useState('')
  const make = useMutation({
    mutationFn: S.newAgentKey,
    onSuccess: r => {
      setCmd(connectCommand(location.origin, r.key))
      qc.setQueryData(Q.agentKey, { exists: true })
    },
    onError: e => toast.error(err(e)),
  })
  const remove = useMutation({
    mutationFn: S.removeAgentKey,
    onSuccess: r => {
      setCmd('')
      qc.setQueryData(Q.agentKey, r)
      toast.success(t('The agent key is removed'))
    },
    onError: e => toast.error(err(e)),
  })
  if (q.isError) return <Body><Problem>{err(q.error)}</Problem></Body>
  if (!q.ready) return <Body><Loading /></Body>
  const exists = q.data.exists
  return (
    <Body>
      <p>
        {cmd
          ? t('Run this once in a terminal. The key is shown only now — copy it:')
          : exists
            ? t('Your agent has a key. Make a new one if you lost it.')
            : t('Make a key, then run the command it shows once in a terminal.')}
      </p>
      {cmd && (
        <div className="space-y-2">
          <pre className="rounded-lg bg-muted p-3 text-sm whitespace-pre-wrap break-all" data-command>
            {cmd}
          </pre>
          <Button
            variant="outline"
            className="h-11 text-base"
            onClick={() =>
              navigator.clipboard.writeText(cmd).then(
                () => toast.success(t('Copied.')),
                () => toast.error(t("Couldn't copy. Select the text and copy it yourself.")),
              )
            }
          >
            <Copy />
            {t('Copy')}
          </Button>
        </div>
      )}
      <div className="flex flex-wrap gap-2">
        <Button className="h-11 text-base whitespace-normal" disabled={make.isPending} onClick={() => make.mutate()}>
          {exists ? t('Make a new key (the old one stops working)') : t('Make a key for your agent')}
        </Button>
        {exists && (
          <Button variant="outline" className="h-11 text-base" disabled={remove.isPending} onClick={() => remove.mutate()}>
            {t('Remove the key')}
          </Button>
        )}
      </div>
    </Body>
  )
}
