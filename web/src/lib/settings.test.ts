import type { TFunction } from 'i18next'
// The settings windows' pure logic: tags, pay, intervals, activity lines,
// Get started and great-match notifications.
import { describe, expect, test } from 'vitest'
import type { Job, Setup } from './api'
import { activityLine, activityText } from './activity'
import { formatAmount, parseAmount, withAmount, withDefaults } from './money'
import { freshMatches, greatMatches } from './notify'
import { ago, checkSummary, everyLabel, intervalOptions, sourceState, type SourceSchedule } from './schedule'
import { addTags, mentionCount, unpicked } from './tags'
import { fileName, stepsLeft } from './windows'

describe('tags', () => {
  test('a typed line splits on commas, semicolons and new lines, trimmed', () => {
    expect(addTags([], ' Kế toán, n8n ;Excel\nZalo ,, ')).toEqual(['Kế toán', 'n8n', 'Excel', 'Zalo'])
  })
  test('a tag already there, in any case, is not added twice', () => {
    expect(addTags(['n8n'], 'N8N, kế toán, Kế Toán')).toEqual(['n8n', 'kế toán'])
  })
  test('the list given is left alone', () => {
    const list = ['a']
    addTags(list, 'b')
    expect(list).toEqual(['a'])
  })
  test('suggestions already picked are not offered', () => {
    expect(unpicked(['Vietnamese', 'English'], ['english'])).toEqual(['Vietnamese'])
  })
  test('the count finds terms without accents, in the title or the posting', () => {
    const jobs = [
      { title: 'Kế toán tổng hợp', jd_full: '' },
      { title: 'Sales', jd_full: 'Biết dùng n8n' },
      { title: 'Driver', jd_full: '' },
    ]
    expect(mentionCount(jobs, ['ke toan', 'N8N'])).toBe(2)
    expect(mentionCount(jobs, [])).toBe(0)
  })
})

describe('money', () => {
  test('amounts read with the locale’s separators', () => {
    expect(formatAmount(20000000, 'en-GB')).toBe('20,000,000')
    expect(formatAmount(20000000, 'vi-VN')).toBe('20.000.000')
    expect(formatAmount(0, 'en-GB')).toBe('')
  })
  test('whatever is typed becomes the number in it', () => {
    expect(parseAmount('20.000.000 đ')).toBe(20000000)
    expect(parseAmount('abc')).toBe(0)
  })
  test('typing an amount replaces the text the profile had', () => {
    expect(withAmount({ amount: 0, text: 'thỏa thuận' }, '15,000,000')).toEqual({ amount: 15000000, text: '' })
    expect(withAmount({ amount: 5, text: 'x' }, '')).toEqual({ amount: 0, text: 'x' })
  })
  test('VND per month until the user says otherwise', () => {
    expect(withDefaults(undefined)).toEqual({ amount: 0, currency: 'VND', period: 'month' })
    expect(withDefaults({ amount: 1, currency: 'USD', period: 'year', basis: 'net' })).toEqual({ amount: 1, currency: 'USD', period: 'year', basis: 'net' })
  })
})

describe('intervals', () => {
  test('off and every interval at least the minimum', () => {
    expect(intervalOptions({ every_minutes: 120, min_every_minutes: 60 }).map(o => o.minutes)).toEqual([0, 60, 120, 360, 720, 1440])
  })
  test('an unusual current value stays on the list, in order', () => {
    const opts = intervalOptions({ every_minutes: 90, min_every_minutes: 60 })
    expect(opts.map(o => o.minutes)).toEqual([0, 60, 90, 120, 360, 720, 1440])
    expect(opts.find(o => o.minutes === 90)?.label).toEqual(['every {n} minutes', { n: 90 }])
  })
  test('labels', () => {
    expect(everyLabel(0)).toEqual(['off'])
    expect(everyLabel(60)).toEqual(['every hour'])
    expect(everyLabel(1440)).toEqual(['once a day'])
  })
  test('how long ago', () => {
    const now = Date.parse('2026-11-07T12:00:00Z')
    expect(ago('2026-11-07T11:59:40Z', now)).toEqual(['just now'])
    expect(ago('2026-11-07T11:59:00Z', now)).toEqual(['{n} minute ago', { n: 1 }])
    expect(ago('2026-11-07T11:55:00Z', now)).toEqual(['{n} minutes ago', { n: 5 }])
    expect(ago('2026-11-07T09:00:00Z', now)).toEqual(['{n} hours ago', { n: 3 }])
    expect(ago('2026-11-05T12:00:00Z', now)).toEqual(['{n} days ago', { n: 2 }])
  })
  const site: SourceSchedule = { source: 'topcv', name: 'TopCV', every_minutes: 60, min_every_minutes: 60, builtin: true, task: false, configured: true }
  const task = { ...site, source: 'jev-rate', name: 'Automatic job rating', task: true }
  test('a site’s failure reads as words; a task says what went wrong', () => {
    const h = { name: 'topcv', count: 0, healthy: false, lastRun: '2026-11-07T11:00:00Z', lastError: 'HTTP 503' }
    expect(sourceState(site, h)).toEqual({ text: ["The last check didn't work"], bad: true })
    expect(sourceState(task, h)).toEqual({ text: ['HTTP 503'], bad: true })
    expect(sourceState(site, undefined).text).toEqual(['Not checked yet'])
    expect(sourceState(task, undefined).text).toEqual(["Hasn't run yet"])
  })
  test('Check now: a site already being checked has not failed', () => {
    const r = checkSummary(
      [
        { status: 'fulfilled', value: { ok: true, result: { added: 2 } } },
        { status: 'fulfilled', value: { ok: false } },
        { status: 'rejected', reason: new Error('that source is already being crawled') },
        { status: 'rejected', reason: new Error('boom') },
      ],
      ['A', 'B', 'C', 'D'],
    )
    expect(r).toEqual({ added: 2, failed: ['B', 'D'] })
  })
})

describe('activity', () => {
  test('server lines become their template and values', () => {
    expect(activityLine('Added "Kế toán"')).toEqual(['Added {job}', { job: '"Kế toán"' }])
    expect(activityLine('Moved "Kế toán" to waiting')).toEqual(['Moved {job} to {stage}', { job: '"Kế toán"', stage: ['Ready to apply'] }])
    expect(activityLine('Rated "X" B 4.2 (claude-opus-5-5)')).toEqual(['Rated {job} {letter} {score} ({rater})', { job: '"X"', letter: 'B', score: 4.2, rater: 'claude-opus-5-5' }])
    expect(activityLine('vietnamworks: 3 new job(s)')).toEqual(['{source}: {n} new jobs', { source: 'VietnamWorks', n: 3 }])
    expect(activityLine('Moved 4 expired job(s) to Closed (drag one back to keep it)')).toEqual(['Moved {n} expired jobs to Closed', { n: 4 }])
    expect(activityLine('Location updated')).toEqual(['Location updated'])
  })
  test('texts inside are translated and the score keeps one decimal', () => {
    const t = (k: string, v?: Record<string, string | number>) => `${k}|${JSON.stringify(v ?? {})}`
    expect(activityText('Moved "X" to applied', t, 'vi-VN')).toBe('Moved {job} to {stage}|{"job":"\\"X\\"","stage":"Applied|{}"}')
    expect(activityText('Rated "X" A 4 (r)', t, 'vi-VN')).toContain('"score":"4,0"')
  })
})

describe('get started', () => {
  const base: Setup = { cv: false, about_me: false, wants: false, search_queries: 0, location: false, topcv: false, done: false }
  test('only the steps left, in order', () => {
    expect(stepsLeft(base).map(s => s.window)).toEqual(['cv', 'profile', 'keywords'])
    expect(stepsLeft({ ...base, cv: true, search_queries: 2 }).map(s => s.key)).toEqual(['wants'])
  })
  test('an About me written by hand counts as the CV', () => {
    expect(stepsLeft({ ...base, about_me: true }).map(s => s.key)).toEqual(['wants', 'search_queries'])
  })
  test('nothing once done', () => {
    expect(stepsLeft({ ...base, done: true })).toEqual([])
  })
})

describe('downloads', () => {
  test('the file name comes from Content-Disposition', () => {
    expect(fileName('attachment; filename="my cv.pdf"', 'cv')).toBe('my cv.pdf')
    expect(fileName("attachment; filename*=UTF-8''Nguy%E1%BB%85n.pdf", 'cv')).toBe('Nguyễn.pdf')
    expect(fileName(null, 'cv')).toBe('cv')
  })
})

describe('great matches', () => {
  const job = (id: number, letter: string, score: number, status: Job['status'] = 'saved') =>
    ({ id, score_letter: letter, score, status, score_status: 'scored' }) as Job
  test('A and B jobs not applied for yet, new ones best first', () => {
    const jobs = [job(1, 'B', 4.0), job(2, 'A', 4.8), job(3, 'C', 3.0), job(4, 'A', 4.9, 'applied'), job(5, 'A', 4.5, 'waiting')]
    expect(greatMatches(jobs).map(j => j.id)).toEqual([1, 2, 5])
    expect(freshMatches(jobs, new Set([2])).map(j => j.id)).toEqual([5, 1])
  })
})

test('server errors read in Vietnamese, numbers and all', async () => {
  const { serverError } = await import('./serverText')
  const vi = (await import('@/locales/vi.json')).default as Record<string, string>
  const t = ((k: string, v?: Record<string, unknown>) => (vi[k] ?? k).replace(/\{(\w+)\}/g, (_, n) => String(v?.[n] ?? ''))) as unknown as TFunction
  expect(serverError("invalid input: reading Facebook is busy right now — try again in 3 minutes, or paste the post's text under the link", t)).toBe(
    'Đang đọc Facebook hơi nhiều, bạn thử lại sau 3 phút, hoặc dán nội dung bài ngay dưới link.',
  )
  expect(serverError('invalid input: paste a job link or description', t)).toBe('Dán link hoặc nội dung tin tuyển dụng.')
  expect(serverError('something new', t)).toBe('something new')
})
