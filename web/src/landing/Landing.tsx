// braveduck's landing page (/welcome): what it is, for whom, and how to start.
// Public: no token, no API calls.
import { useEffect } from 'react'
import {
  ArrowRight, Ban, Bot, FileText, Globe, HardDrive, KeyRound, MessageCircle, MousePointerClick,
  ShieldCheck, Smartphone, Sparkles, Sun,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { LANGS, lang, setLang, type Lang } from '@/lib/i18n'
import { N_ } from '@/lib/n'
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from '@/components/ui/accordion'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Preview } from './Preview'

const REPO = 'https://github.com/khangpworking/braveduck-cli'

/** GitHub's mark (lucide no longer ships brand logos). */
function Github({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden className={className}>
      <path d="M12 .3a12 12 0 0 0-3.8 23.4c.6.1.8-.3.8-.6v-2.2c-3.3.7-4-1.4-4-1.4-.6-1.4-1.4-1.8-1.4-1.8-1-.7.1-.7.1-.7 1.2.1 1.8 1.2 1.8 1.2 1 1.8 2.8 1.3 3.5 1 0-.8.4-1.3.7-1.6-2.7-.3-5.5-1.3-5.5-6 0-1.2.5-2.3 1.3-3.1-.2-.4-.6-1.6 0-3.2 0 0 1-.3 3.3 1.2a11.5 11.5 0 0 1 6 0C17.3 4.7 18.3 5 18.3 5c.7 1.6.2 2.9.1 3.2.8.8 1.3 1.9 1.3 3.1 0 4.6-2.8 5.6-5.5 5.9.4.4.8 1.1.8 2.2v3.3c0 .3.2.7.8.6A12 12 0 0 0 12 .3" />
    </svg>
  )
}

const FEATURES = [
  { icon: Sparkles, title: N_('Every job graded A to F'), text: N_('Graded against your CV and what you want, with the reasons in plain words. The best fits come first.') },
  { icon: Sun, title: N_('Today, at a glance'), text: N_('Postings closing this week, applications waiting for an answer and the best new matches. Nothing else.') },
  { icon: Ban, title: N_('Deal-breakers up front'), text: N_('Saturday work, unstated pay, a long commute: flagged in red before you spend time on a job.') },
  { icon: MessageCircle, title: N_('Never forget to follow up'), text: N_('A week without an answer? braveduck reminds you and writes the message for you to send.') },
  { icon: FileText, title: N_('A CV and an email for each job'), text: N_('Your own AI tailors your CV and writes the email; braveduck keeps every version.') },
  { icon: Smartphone, title: N_('Made for your phone too'), text: N_('Big text, big buttons, one job per screen. The same board on a laptop.') },
]

const STEPS = [
  { title: N_('Upload your CV'), text: N_('braveduck reads it and fills in your profile. Say how much you want to earn and how far you’ll travel.') },
  { title: N_('braveduck finds and grades jobs'), text: N_('Every few hours it searches the job sites for your terms, skips duplicates and grades what’s new.') },
  { title: N_('You apply; braveduck keeps track'), text: N_('Open a posting, send your CV, tap “Yes, I applied”. It tells you when to follow up.') },
]

const PRIVACY = [
  { icon: HardDrive, title: N_('Runs on your computer'), text: N_('Your jobs, CV and notes live in one file on your disk. Or run it for a group on your own server.') },
  { icon: KeyRound, title: N_('No stored passwords'), text: N_('You log in to job sites yourself, in a real browser window. braveduck keeps only the encrypted session.') },
  { icon: MousePointerClick, title: N_('You always press submit'), text: N_('braveduck opens the application with your details ready. It never applies for you.') },
  { icon: Github, title: N_('Open source'), text: N_('Every line is on GitHub: see what it does, or change it.') },
]

const FAQ = [
  { q: N_('Is braveduck free?'), a: N_('Yes. It’s open source and runs on your own computer, so there is nothing to pay for.') },
  { q: N_('Which job sites does it read?'), a: N_('It searches VietnamWorks and CareerLink by itself. Paste a link from TopCV, JobOKO or a Facebook post and it adds that job too, and it can follow public Facebook groups.') },
  { q: N_('Who grades the jobs?'), a: N_('Jev, a decision model from TypeSafe, by default, or your own AI (Claude Code or any chat). You can turn Jev off in Settings.') },
  { q: N_('Does it apply for me?'), a: N_('No. It opens the posting with your login and CV ready; you check it and press submit yourself.') },
  { q: N_('Can I use it on my phone?'), a: N_('Yes. Open the board from your phone on your home Wi-Fi, or run it on a server for your group.') },
]

const SOURCES = ['TopCV', 'VietnamWorks', 'CareerLink', 'JobOKO', N_('Facebook groups')]

function Section({ id, kicker, title, intro, children }: { id: string; kicker: string; title: string; intro?: string; children: React.ReactNode }) {
  return (
    <section id={id} className="scroll-mt-20 py-20 md:py-28">
      <div className="mx-auto max-w-6xl px-5">
        <div className="mx-auto mb-12 max-w-2xl text-center">
          <p className="mb-3 text-sm font-semibold tracking-wide text-warning uppercase">{kicker}</p>
          <h2 className="text-3xl font-semibold tracking-tight md:text-4xl">{title}</h2>
          {intro && <p className="mt-4 text-body text-muted-foreground">{intro}</p>}
        </div>
        {children}
      </div>
    </section>
  )
}

function LangSwitch() {
  const other = (lang === 'vi' ? 'en' : 'vi') as Lang
  return (
    <Button variant="ghost" size="sm" onClick={() => setLang(other)} aria-label={LANGS[other]}>
      <Globe />
      {LANGS[other]}
    </Button>
  )
}

export function Landing() {
  const { t } = useTranslation()
  useEffect(() => {
    document.title = t('braveduck — job hunting, sorted')
  }, [t])
  return (
    <div className="min-h-dvh bg-background text-foreground">
      <header className="sticky top-0 z-20 border-b bg-background/85 backdrop-blur">
        <nav className="mx-auto flex h-16 max-w-6xl items-center gap-6 px-5" aria-label={t('Main menu')}>
          <a href="#top" className="flex items-center gap-2 text-lg font-bold">
            <span aria-hidden>🦆</span> braveduck
          </a>
          <div className="hidden flex-1 items-center gap-6 text-sm text-muted-foreground md:flex">
            <a href="#features" className="hover:text-foreground">{t('Features')}</a>
            <a href="#how" className="hover:text-foreground">{t('How it works')}</a>
            <a href="#privacy" className="hover:text-foreground">{t('Privacy')}</a>
            <a href="#faq" className="hover:text-foreground">{t('Questions')}</a>
          </div>
          <div className="ml-auto flex items-center gap-2">
            <LangSwitch />
            <Button asChild size="sm" className="hidden sm:inline-flex">
              <a href="/">{t('Open my board')}</a>
            </Button>
          </div>
        </nav>
      </header>

      <main id="top">
        <section className="relative overflow-hidden">
          <div className="bg-grid pointer-events-none absolute inset-0" />
          <div className="pointer-events-none absolute inset-x-0 top-0 h-96 bg-linear-to-b from-brand-muted to-transparent" />
          <div className="relative mx-auto grid max-w-6xl items-center gap-12 px-5 pt-16 pb-20 md:pt-24 lg:grid-cols-12">
            <div className="min-w-0 lg:col-span-5">
              <Badge variant="outline" className="mb-6">
                <span className="size-2 rounded-full bg-grade-a" />
                {t('Free and open source · runs on your computer')}
              </Badge>
              <h1 className="text-4xl leading-tight font-semibold tracking-tight md:text-5xl md:leading-tight xl:text-6xl">
                {t('Job hunting, sorted.')}{' '}
                <span className="text-warning">{t('A few minutes a day.')}</span>
              </h1>
              <p className="mt-6 max-w-xl text-body text-muted-foreground md:text-lg">
                {t('braveduck gathers job posts from TopCV, VietnamWorks, CareerLink and Facebook groups, grades each one against your CV, and tells you what to do today.')}
              </p>
              <div className="mt-8 flex flex-wrap gap-3">
                <Button asChild size="lg">
                  <a href="#start">
                    {t('Get started free')}
                    <ArrowRight />
                  </a>
                </Button>
                <Button asChild size="lg" variant="outline">
                  <a href="#how">{t('See how it works')}</a>
                </Button>
              </div>
              <p className="mt-6 flex items-center gap-2 text-sm text-muted-foreground">
                <ShieldCheck className="size-4 text-success" />
                {t('No account. No stored passwords. You press submit.')}
              </p>
            </div>
            <div className="min-w-0 lg:col-span-7">
              <Preview />
            </div>
          </div>
        </section>

        <div className="border-y bg-muted/40">
          <div className="mx-auto flex max-w-6xl flex-wrap items-center justify-center gap-x-10 gap-y-3 px-5 py-6 text-sm text-muted-foreground">
            <span>{t('Reads job posts from')}</span>
            {SOURCES.map(s => (
              <span key={s} className="text-body font-semibold text-foreground/70">
                {t(s)}
              </span>
            ))}
          </div>
        </div>

        <Section id="features" kicker={t('Features')} title={t('Everything a job hunt needs, nothing it doesn’t')} intro={t('Made for people who want a job, not another app to learn: big, clear and in Vietnamese.')}>
          <div className="grid gap-5 sm:grid-cols-2 lg:grid-cols-3">
            {FEATURES.map(f => (
              <Card key={f.title}>
                <CardHeader>
                  <span className="mb-3 flex size-11 items-center justify-center rounded-xl bg-brand-muted ring-1 ring-brand/40">
                    <f.icon className="size-5 text-brand-foreground" />
                  </span>
                  <CardTitle>{t(f.title)}</CardTitle>
                  <CardDescription>{t(f.text)}</CardDescription>
                </CardHeader>
              </Card>
            ))}
          </div>
        </Section>

        <section id="how" className="scroll-mt-20 border-y bg-muted/40 py-20 md:py-28">
          <div className="mx-auto max-w-6xl px-5">
            <div className="mx-auto mb-12 max-w-2xl text-center">
              <p className="mb-3 text-sm font-semibold tracking-wide text-warning uppercase">{t('How it works')}</p>
              <h2 className="text-3xl font-semibold tracking-tight md:text-4xl">{t('Three steps, then it runs by itself')}</h2>
            </div>
            <ol className="grid gap-8 md:grid-cols-3">
              {STEPS.map((s, i) => (
                <li key={s.title} className="relative">
                  <span className="mb-4 flex size-12 items-center justify-center rounded-full bg-foreground text-xl font-semibold text-background">{i + 1}</span>
                  <h3 className="text-xl font-semibold">{t(s.title)}</h3>
                  <p className="mt-2 text-muted-foreground">{t(s.text)}</p>
                </li>
              ))}
            </ol>
          </div>
        </section>

        <Section id="privacy" kicker={t('Privacy')} title={t('Your job hunt stays yours')}>
          <div className="grid gap-x-10 gap-y-8 md:grid-cols-2">
            {PRIVACY.map(p => (
              <div key={p.title} className="flex gap-4">
                <span className="flex size-11 shrink-0 items-center justify-center rounded-xl border">
                  <p.icon className="size-5" />
                </span>
                <div>
                  <h3 className="text-lg font-semibold">{t(p.title)}</h3>
                  <p className="mt-1 text-muted-foreground">{t(p.text)}</p>
                </div>
              </div>
            ))}
          </div>
        </Section>

        <section className="px-5">
          <div className="mx-auto grid max-w-6xl items-center gap-8 rounded-3xl border bg-muted/40 p-8 md:grid-cols-2 md:p-12">
            <div>
              <Badge variant="secondary" className="mb-4">
                <Bot />
                MCP
              </Badge>
              <h2 className="text-2xl font-semibold tracking-tight md:text-3xl">{t('Works with your AI')}</h2>
              <p className="mt-3 text-muted-foreground">
                {t('Connect Claude Code, Codex or any MCP agent and ask it to rate jobs, tailor your CV or draft emails, right on your board.')}
              </p>
            </div>
            <pre className="overflow-x-auto rounded-xl bg-foreground p-5 text-sm text-background">
              <code>{'claude mcp add braveduck -- braveduck mcp\n\n> '}{t('Rate my new jobs and tailor my CV for the best one')}</code>
            </pre>
          </div>
        </section>

        <Section id="start" kicker={t('Get started')} title={t('Start in two minutes')} intro={t('Install it, start the board, and follow “Get started”: upload your CV, say what you want, add search terms.')}>
          <div className="mx-auto max-w-3xl">
            <pre className="overflow-x-auto rounded-xl bg-foreground p-5 text-sm leading-relaxed text-background">
              <code>
                {'# ' + t('needs Go 1.25 or newer') + '\ngo install github.com/khangpworking/braveduck-cli/cmd/braveduck@latest\nbraveduck serve   # → http://127.0.0.1:8080'}
              </code>
            </pre>
            <div className="mt-6 flex flex-wrap justify-center gap-3">
              <Button asChild size="lg">
                <a href="/">
                  {t('Open my board')}
                  <ArrowRight />
                </a>
              </Button>
              <Button asChild size="lg" variant="outline">
                <a href={REPO} target="_blank" rel="noopener noreferrer">
                  <Github />
                  {t('View on GitHub')}
                </a>
              </Button>
            </div>
          </div>
        </Section>

        <section id="faq" className="scroll-mt-20 border-t py-20 md:py-28">
          <div className="mx-auto max-w-3xl px-5">
            <h2 className="mb-8 text-center text-3xl font-semibold tracking-tight md:text-4xl">{t('Questions')}</h2>
            <Accordion type="single" collapsible>
              {FAQ.map(f => (
                <AccordionItem key={f.q} value={f.q}>
                  <AccordionTrigger>{t(f.q)}</AccordionTrigger>
                  <AccordionContent>{t(f.a)}</AccordionContent>
                </AccordionItem>
              ))}
            </Accordion>
          </div>
        </section>

        <section className="px-5 pb-20">
          <div className="relative mx-auto max-w-6xl overflow-hidden rounded-3xl bg-foreground px-8 py-14 text-center text-background md:py-20">
            <div className="bg-grid pointer-events-none absolute inset-0 opacity-30" />
            <h2 className="relative mx-auto max-w-2xl text-3xl font-semibold tracking-tight md:text-4xl">
              {t('Spend less time searching and more time applying.')}
            </h2>
            <div className="relative mt-8 flex justify-center">
              <Button asChild size="lg" variant="secondary">
                <a href="#start">
                  {t('Get started free')}
                  <ArrowRight />
                </a>
              </Button>
            </div>
          </div>
        </section>
      </main>

      <footer className="border-t">
        <div className="mx-auto flex max-w-6xl flex-col gap-4 px-5 py-8 text-sm text-muted-foreground md:flex-row md:items-center">
          <span className="font-semibold text-foreground">🦆 braveduck</span>
          <span>{t('Made for job seekers in Vietnam.')}</span>
          <div className="flex flex-wrap gap-5 md:ml-auto">
            <a href={REPO} target="_blank" rel="noopener noreferrer" className="hover:text-foreground">GitHub</a>
            <a href="/" className="hover:text-foreground">{t('Open my board')}</a>
            <span>© 2026 braveduck · MIT</span>
          </div>
        </div>
      </footer>
    </div>
  )
}
