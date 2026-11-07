import { fold } from './jobs'

/**
 * Adds what was typed into a tags field: "a, b; c" or pasted lines become
 * separate tags, trimmed, and a tag already there (in any case) isn't added
 * twice. Returns a new list.
 */
export function addTags(list: string[], typed: string): string[] {
  const out = [...list]
  for (const part of typed.split(/[,;\n]/)) {
    const v = part.trim()
    if (v && !out.some(x => x.toLowerCase() === v.toLowerCase())) out.push(v)
  }
  return out
}

/** The suggestions not yet picked, compared without case. */
export const unpicked = (suggestions: string[], list: string[]) =>
  suggestions.filter(s => !list.some(v => v.toLowerCase() === s.toLowerCase()))

/** How many jobs mention any of the terms in the title or the posting ("ke toan" finds "Kế toán"). */
export function mentionCount(jobs: { title: string; jd_full: string }[], terms: string[]) {
  const folded = terms.map(fold).filter(Boolean)
  if (!folded.length) return 0
  return jobs.filter(j => {
    const text = fold(`${j.title} ${j.jd_full || ''}`)
    return folded.some(t => text.includes(t))
  }).length
}
