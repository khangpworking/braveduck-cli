// The board speaks Vietnamese by default and English on request, like the old
// board: English text is the key, so t("Add job") falls back to English when a
// translation is missing. Placeholders are {name}. The choice is saved under
// the same localStorage key the old board used; the server's default
// (BRAVEDUCK_LANG) arrives as <html lang>.
import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import vi from '@/locales/vi.json'

export const LANGS = { vi: 'Tiếng Việt', en: 'English' } as const
export type Lang = keyof typeof LANGS
const KEY = 'braveduck.lang'

function pick(): Lang {
  let saved = ''
  try {
    saved = localStorage.getItem(KEY) ?? ''
  } catch {
    // storage blocked: use the server's default
  }
  const served = document.documentElement.lang
  if (saved in LANGS) return saved as Lang
  return served in LANGS ? (served as Lang) : 'vi'
}

export const lang = pick()
document.documentElement.lang = lang

void i18n.use(initReactI18next).init({
  lng: lang,
  fallbackLng: false,
  resources: { vi: { translation: vi } },
  keySeparator: false,
  nsSeparator: false,
  returnEmptyString: false,
  interpolation: { escapeValue: false, prefix: '{', suffix: '}' },
})

/** Saves a language and reloads, so everything redraws in it. */
export function setLang(l: Lang) {
  try {
    localStorage.setItem(KEY, l)
  } catch {
    // nothing to save to; the reload still switches for this visit
  }
  location.reload()
}

export const locale = () => (lang === 'vi' ? 'vi-VN' : 'en-GB')
export default i18n

export { N_ } from './n'
