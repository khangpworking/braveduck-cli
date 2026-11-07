// Recent activity: the server writes each line in English from a few
// templates (Added "…", Moved "…" to applied, …). These turn a line back
// into its template and values, so it can be shown in the board's language.
import type { Status } from './api'
import { sourceName, stageName } from './jobs'
import { N_ } from './n'

export interface ActivityEvent {
  type: string // error / info / added, moved, rated, …
  message: string
  timestamp: string
}

/** A text to translate, with values; a value that is itself a text is a [key]. */
export type Part = string | number | [string]
export type Line = [string, Record<string, Part>?]

const PATTERNS: [RegExp, string, (m: RegExpExecArray) => Record<string, Part>][] = [
  [/^Added (".*")$/, N_('Added {job}'), ([, job]) => ({ job })],
  [/^Removed (".*")$/, N_('Removed {job}'), ([, job]) => ({ job })],
  [/^Moved (".*") to (saved|waiting|applied|interview|offer|closed)$/, N_('Moved {job} to {stage}'), ([, job, stage]) => ({ job, stage: [stageName(stage as Status)] })],
  [/^Rated (".*") ([A-F]) ([\d.]+) \((.*)\)$/, N_('Rated {job} {letter} {score} ({rater})'), ([, job, letter, score, rater]) => ({ job, letter, score: Number(score), rater })],
  [/^Tailored CV v(\d+) for (".*")$/, N_('Tailored CV v{n} for {job}'), ([, n, job]) => ({ n: Number(n), job })],
  [/^Email draft ready for (".*")$/, N_('Email draft ready for {job}'), ([, job]) => ({ job })],
  [/^Application email sent for (".*")$/, N_('Application email sent for {job}'), ([, job]) => ({ job })],
  [/^Applied on (.*): (".*")$/, N_('Applied on {site}: {job}'), ([, site, job]) => ({ site, job })],
  [/^Login to (.+) failed: (.*)$/, N_('Login to {site} failed: {error}'), ([, site, error]) => ({ site, error: [error] })],
  [/^Connected (.+)$/, N_('Connected {site}'), ([, site]) => ({ site })],
  [/^Disconnected (.+)$/, N_('Disconnected {site}'), ([, site]) => ({ site })],
  [/^Following the Facebook group (.+)$/, N_('Following the Facebook group {url}'), ([, url]) => ({ url })],
  [/^Moved (\d+) expired job\(s\) to Closed \(drag one back to keep it\)$/, N_('Moved {n} expired jobs to Closed'), ([, n]) => ({ n: Number(n) })],
  [/^(.+): (\d+) new job\(s\)$/, N_('{source}: {n} new jobs'), ([, source, n]) => ({ source: sourceName(source), n: Number(n) })],
  [/^(.+): (\d+) new job post\(s\)$/, N_('{source}: {n} new job posts'), ([, source, n]) => ({ source, n: Number(n) })],
  [/^Uploaded your CV (".*") — its text is in your profile$/, N_('Uploaded your CV {file} — its text is in your profile'), ([, file]) => ({ file })],
  [/^TopCV sync: (\d+) updated, (\d+) moved to Applied, (\d+) added$/, N_('TopCV sync: {updated} updated, {moved} moved to Applied, {added} added'), ([, updated, moved, added]) => ({ updated: Number(updated), moved: Number(moved), added: Number(added) })],
  [/^(.+) is a private group: only its members can see its posts, so paste the ones you want$/, N_('{group} is a private group: only its members can see its posts, so paste the ones you want'), ([, group]) => ({ group })],
]

/** One activity line as a text to translate and its values. */
export function activityLine(message: string): Line {
  for (const [re, key, vars] of PATTERNS) {
    const m = re.exec(message)
    if (m) return [key, vars(m)]
  }
  return [message]
}

/**
 * Fills a line in: texts inside are translated, and a rating's score shows
 * one decimal in the reader's way of writing numbers.
 */
export function activityText(message: string, t: (k: string, v?: Record<string, string | number>) => string, locale: string) {
  const [key, vars] = activityLine(message)
  const out: Record<string, string | number> = {}
  for (const [k, v] of Object.entries(vars ?? {})) {
    if (Array.isArray(v)) out[k] = t(v[0])
    else if (k === 'score' && typeof v === 'number') out[k] = v.toLocaleString(locale, { minimumFractionDigits: 1, maximumFractionDigits: 1 })
    else out[k] = typeof v === 'number' ? v.toLocaleString(locale) : v
  }
  return t(key, out)
}
