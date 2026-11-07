// A placeholder named like one of i18next's own options is read as that
// option, not filled in: t("… {lng}", { lng }) switches the language and
// shows the English. No text may use those names.
import { expect, test } from 'vitest'
import vi from '@/locales/vi.json'

const RESERVED = ['lng', 'lngs', 'ns', 'context', 'count', 'ordinal', 'defaultValue', 'replace', 'returnObjects', 'joinArrays', 'postProcess', 'interpolation', 'keyPrefix']

test('placeholders avoid the names i18next reserves', () => {
  const bad = Object.keys(vi).filter(k => [...k.matchAll(/\{(\w+)\}/g)].some(m => RESERVED.includes(m[1])))
  expect(bad).toEqual([])
})
