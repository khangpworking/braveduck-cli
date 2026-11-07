// Pay as the profile form edits it (service.Money).

export interface Money {
  amount: number // 0 = not given
  currency?: string
  period?: string // month / year / hour
  basis?: string // gross / net / ""
  text?: string // what the profile said, when the fields can't show it
}

/** 20000000 → "20,000,000" (or "20.000.000" in Vietnamese); 0 → "". */
export const formatAmount = (n: number, locale: string) => (n ? n.toLocaleString(locale) : '')

/** Whatever was typed → the whole number in it: "20.000.000 đ" → 20000000. */
export const parseAmount = (s: string) => parseInt(s.replace(/\D/g, ''), 10) || 0

/**
 * The money after typing an amount: a new figure replaces the text the
 * profile had, and VND per month is assumed until the user says otherwise.
 */
export function withAmount(m: Money, typed: string): Money {
  const amount = parseAmount(typed)
  return { ...m, amount, text: amount ? '' : m.text }
}

/** Fills the choices the form always shows (VND, per month). */
export const withDefaults = (m: Money | undefined): Money => ({
  ...(m ?? { amount: 0 }),
  currency: m?.currency || 'VND',
  period: m?.period || 'month',
})
