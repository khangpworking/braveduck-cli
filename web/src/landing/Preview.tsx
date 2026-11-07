// A picture of the board for the landing page, drawn with the board's own
// parts and made-up jobs, so it stays sharp and speaks the reader's language.
import { ArrowUpRight, Ban, ChevronRight, MessageCircle, Sun } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { cn } from '@/lib/utils'

const ROWS = [
  { g: 'A', tone: 'bg-grade-a', title: 'Kế toán tổng hợp', company: 'Thực phẩm Hoa Sen Vàng', pay: '14–18 triệu · 2 km', on: true },
  { g: 'B', tone: 'bg-grade-b', title: 'Nhân viên kế toán công nợ', company: 'Logistics Cảng Xanh', pay: '12–15 triệu · 8 km' },
  { g: 'C', tone: 'bg-grade-c', title: 'Nhân viên hành chính', company: 'May Thành Phát', pay: '10–12 triệu · 6 km', warn: true },
]

export function Preview() {
  const { t } = useTranslation()
  return (
    <div className="min-w-0 overflow-hidden rounded-2xl border bg-background shadow-2xl ring-1 ring-foreground/5" aria-hidden>
      <div className="flex items-center gap-1.5 border-b bg-muted/60 px-4 py-2.5">
        <span className="size-3 rounded-full bg-danger/70" />
        <span className="size-3 rounded-full bg-brand" />
        <span className="size-3 rounded-full bg-grade-a/70" />
        <span className="ml-3 truncate rounded-md bg-background px-3 py-0.5 text-xs text-muted-foreground">127.0.0.1:8080</span>
      </div>
      <div className="grid sm:grid-cols-2">
        <div className="min-w-0 space-y-3 p-4 sm:border-r">
          <Card size="sm" tone="warning">
            <div className="space-y-1 px-3">
              <span className="flex items-center gap-2 text-sm font-semibold">
                <Sun className="size-4 text-warning" />
                {t('Today')}
              </span>
              <div className="flex items-center gap-2 rounded-lg bg-background/80 p-2">
                <MessageCircle className="size-4 shrink-0 text-warning" />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm font-semibold">{t('Ask {company} for an answer', { company: 'Du lịch Sen Việt' })}</span>
                  <span className="block truncate text-xs text-muted-foreground">{t('No answer for {n} days · {title}', { n: 8, title: 'Nhân viên kế toán' })}</span>
                </span>
                <ChevronRight className="size-4 text-muted-foreground" />
              </div>
            </div>
          </Card>
          <div className="divide-y rounded-xl border">
            {ROWS.map(r => (
              <div key={r.title} className={cn('flex items-center gap-3 p-3', r.on && 'bg-muted')}>
                <span className={cn('flex size-10 shrink-0 items-center justify-center rounded-lg text-lg font-bold text-white', r.tone)}>{r.g}</span>
                <span className="min-w-0">
                  <span className="block truncate text-sm font-semibold">{r.title}</span>
                  <span className="block truncate text-xs text-muted-foreground">{r.company}</span>
                  <span className="block text-xs">{r.pay}</span>
                  {r.warn && (
                    <span className="flex items-center gap-1 text-xs font-semibold text-danger">
                      <Ban className="size-3" />
                      {t('Works on Saturdays')}
                    </span>
                  )}
                </span>
              </div>
            ))}
          </div>
        </div>
        <div className="hidden min-w-0 flex-col gap-3 p-5 sm:flex">
          <div>
            <p className="text-xl font-semibold tracking-tight">Kế toán tổng hợp</p>
            <p className="text-sm text-muted-foreground">Công ty TNHH Thực phẩm Hoa Sen Vàng</p>
          </div>
          <Card size="callout" tone="warning">
            <span className="font-semibold">{t('{n} days left to apply', { n: 2 })}</span>
            <span className="text-sm text-muted-foreground">{t('Apply early so you don’t miss it.')}</span>
          </Card>
          <div className="flex items-center gap-3 rounded-xl bg-muted/70 p-3">
            <span className="flex size-12 items-center justify-center rounded-xl bg-grade-a text-2xl font-bold text-white">A</span>
            <span className="font-semibold">{t('Great fit')}</span>
          </div>
          <ul className="space-y-1.5 text-sm">
            {['Đúng nghề: bạn đã làm kế toán 3 năm', 'Lương cao hơn mức bạn muốn', 'Cách nhà 2 km, nghỉ thứ 7'].map(w => (
              <li key={w} className="flex items-center gap-2">
                <ChevronRight className="size-3.5 text-muted-foreground" />
                {w}
              </li>
            ))}
          </ul>
          <Button className="mt-auto w-full" tabIndex={-1}>
            {t('Open the posting to apply')}
            <ArrowUpRight />
          </Button>
        </div>
      </div>
    </div>
  )
}
