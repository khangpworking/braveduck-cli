// My CV: one line saying what's there, and one button (or a drop) that
// uploads at once. Its text fills in "About me"; replacing one the user
// wrote asks first.
import { useRef, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { CircleCheck, Download, Upload } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { ApiError } from '@/lib/api'
import { Q, S, download, uploadCV } from '@/lib/settings'
import { ago } from '@/lib/schedule'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { confirm } from './Confirm'
import { Body, CloseButton, Footer, Loading, Problem, Win, useErr, useFresh, useMsg, type WinProps } from './parts'

const ACCEPT = '.pdf,.docx,.md,.markdown,.html,.htm'

export function CVWindow(p: WinProps) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [over, setOver] = useState(false)
  const [problem, setProblem] = useState('')
  const up = useMutation({
    mutationFn: async (file: File) => {
      try {
        return await uploadCV(file, false)
      } catch (e) {
        if (!(e instanceof ApiError && e.status === 409)) throw e
        const ok = await confirm({
          title: t(e.message),
          body: t('OK replaces it with the text of {file}.', { file: file.name }),
          ok: t('Replace it'),
        })
        if (!ok) return null
        return uploadCV(file, true)
      }
    },
    onMutate: () => setProblem(''),
    onSuccess: cv => {
      if (!cv) return
      for (const k of [Q.cv, Q.setup, Q.profile, Q.profileForm]) void qc.invalidateQueries({ queryKey: k })
      toast.success(t('CV added. Your profile is updated.'))
    },
    onError: (e: Error) => {
      // Most CV errors are translated without the "invalid input: " in front.
      const full = t(e.message)
      setProblem(full !== e.message ? full : t(e.message.replace(/^invalid input: /, '')))
    },
  })
  const upload = (f: File) => !up.isPending && up.mutate(f)
  const hasFile = (e: React.DragEvent) => [...e.dataTransfer.types].includes('Files')
  return (
    <Win
      open={p.open}
      onOpenChange={o => {
        if (!o) setProblem('')
        p.onOpenChange(o)
      }}
      title={t('My CV')}
      // The whole window takes the drop: a near miss mustn't open the file in this tab.
      onDragOver={e => {
        if (!hasFile(e)) return
        e.preventDefault()
        setOver(true)
      }}
      onDragLeave={e => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setOver(false)
      }}
      onDrop={e => {
        if (!hasFile(e)) return
        e.preventDefault()
        setOver(false)
        const f = e.dataTransfer.files[0]
        if (f) upload(f)
      }}
    >
      <CVBody over={over} upload={upload} pending={up.isPending} problem={problem} />
      <Footer>
        <CloseButton />
      </Footer>
    </Win>
  )
}

function CVBody({ over, upload, pending, problem }: { over: boolean; upload: (f: File) => void; pending: boolean; problem: string }) {
  const { t } = useTranslation()
  const err = useErr()
  const msg = useMsg()
  const q = useFresh(Q.cv, S.cv)
  const input = useRef<HTMLInputElement>(null)


  if (q.isError) return <Body><Problem>{t("Couldn't load your CV: {error}", { error: err(q.error) })}</Problem></Body>
  if (!q.ready) return <Body><Loading /></Body>
  const cv = q.data.cv

  return (
    <Body>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1" data-cv-status>
        {cv ? (
          <>
            <span className="flex items-center gap-2">
              <CircleCheck className="size-5 text-success" />
              {t('{name}, added {time}', { name: cv.name, time: msg(ago(cv.uploaded_at)) })}
            </span>
            <Button
              variant="quiet"
              className="h-10"
              onClick={() => download('/cv/file', cv.name).catch(e => toast.error(err(e)))}
            >
              <Download />
              {t('Download')}
            </Button>
          </>
        ) : (
          <span>{t("Add your CV and we'll fill in your profile from it.")}</span>
        )}
      </div>
      <div
        className={cn(
          'flex flex-col items-center gap-2 rounded-xl border-2 border-dashed px-4 py-6 text-center transition-colors',
          over ? 'border-primary bg-primary/5' : 'border-muted-foreground/30',
        )}
      >
        <input
          ref={input}
          type="file"
          accept={ACCEPT}
          hidden
          aria-label={t('Your CV file')}
          onChange={e => {
            const f = e.target.files?.[0]
            e.target.value = '' // so picking the same file again still counts
            if (f) upload(f)
          }}
        />
        <Button size="lg" disabled={pending} onClick={() => input.current?.click()}>
          <Upload />
          {pending ? t('Uploading…') : cv ? t('Replace it') : t('Choose file')}
        </Button>
        <span className="text-muted-foreground">{t('or drop it here')}</span>
        <span className="text-small text-muted-foreground">{t('PDF or Word, up to 5 MB.')}</span>
      </div>
      {problem && <Problem>{problem}</Problem>}
    </Body>
  )
}
