// Every English text the board shows has a Vietnamese translation, and
// vi.json has no leftovers. Texts are found where they're written: t("…")
// and N_("…") calls with a literal string.
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { expect, test } from 'vitest'
import vi from '@/locales/vi.json'

const SRC = join(import.meta.dirname, '..')

function files(dir: string): string[] {
  return readdirSync(dir).flatMap(f => {
    const p = join(dir, f)
    if (statSync(p).isDirectory()) return f === 'ui' ? [] : files(p)
    return /\.tsx?$/.test(f) && !f.endsWith('.test.ts') ? [p] : []
  })
}

export function keys() {
  const found = new Set<string>()
  const call = /\b(?:t|N_)\(\s*(['"])((?:\\.|(?!\1).)*)\1/g
  for (const f of files(SRC)) {
    for (const m of readFileSync(f, 'utf8').matchAll(call)) found.add(m[2].replace(/\\(.)/g, '$1'))
  }
  return found
}

test('every text has a Vietnamese translation', () => {
  const dict = vi as Record<string, string>
  const missing = [...keys()].filter(k => !dict[k]?.trim())
  expect(missing).toEqual([])
})

test('vi.json has no texts the board no longer uses', () => {
  const used = keys()
  expect(Object.keys(vi).filter(k => !used.has(k))).toEqual([])
})

test('placeholders match', () => {
  const names = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map(m => m[1]).sort()
  const bad = Object.entries(vi as Record<string, string>).filter(([en, v]) => names(en).join() !== names(v).join())
  expect(bad).toEqual([])
})
