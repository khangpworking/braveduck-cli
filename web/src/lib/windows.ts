// The settings windows' names, the Get started steps and other pure bits,
// apart from the API so they're easy to test (lib/settings.ts uses them).
import type { Setup } from './api'
import { N_ } from './n'

/** Every settings window. Open one from anywhere with openWindow(name). */
export type WindowName =
  | 'profile'
  | 'cv'
  | 'keywords'
  | 'sources'
  | 'location'
  | 'rating'
  | 'logins'
  | 'facebook'
  | 'activity'
  | 'agent'
  | 'language'

export const OPEN_EVENT = 'braveduck:open'

/** Opens a settings window, the same as picking it in Settings. */
export const openWindow = (w: WindowName) => window.dispatchEvent(new CustomEvent(OPEN_EVENT, { detail: w }))

/** The file name a download comes with, from Content-Disposition. */
export function fileName(disposition: string | null, fallback: string) {
  const m = /filename\*?=(?:UTF-8'')?"?([^";]+)/i.exec(disposition ?? '')
  if (!m) return fallback
  try {
    return decodeURIComponent(m[1])
  } catch {
    return m[1]
  }
}

export interface Step {
  key: 'cv' | 'wants' | 'search_queries'
  title: string
  window: WindowName
}

const STEPS: Step[] = [
  { key: 'cv', title: N_('Upload your CV'), window: 'cv' },
  { key: 'wants', title: N_("Say what you're looking for"), window: 'profile' },
  { key: 'search_queries', title: N_('Add search terms'), window: 'keywords' },
]

/** The Get started steps still to do; an "About me" written by hand counts as the CV. */
export function stepsLeft(s: Setup): Step[] {
  if (s.done) return []
  const done = { cv: s.cv || s.about_me, wants: s.wants, search_queries: s.search_queries > 0 }
  return STEPS.filter(st => !done[st.key])
}

/** Keywords lists, other than the search terms themselves (keyword.Lists). */
export const FILTERS: [string, string, string?][] = [
  ['negative_content', N_('Hide jobs that mention…'), N_('In the title or the description.')],
  ['negative_title', N_('Hide jobs with these words in the title')],
  ['negative_company', N_('Hide jobs from these companies')],
  ['positive_title', N_('Only keep jobs with these words in the title')],
]
