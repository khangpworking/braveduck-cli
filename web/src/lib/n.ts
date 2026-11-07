/**
 * Marks text that is translated later, where it's used: N_("Saved") is just
 * "Saved", but the i18n test (i18n.test.ts) finds it and checks that vi.json
 * has it. Its own module, so pure code can use it without starting i18next.
 */
export const N_ = <T extends string>(s: T) => s
