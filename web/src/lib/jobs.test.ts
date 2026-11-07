import { expect, test } from 'vitest'
import type { Job, TodayGroup } from './api'
import { dealbreakerText, matches, pay, payUndisclosed, shortCompany, sortJobs, todayEntries } from './jobs'

const job = (over: Partial<Job>): Job =>
  ({
    id: 1, title: '', company: '', url: '', location: '', status: 'saved', pinned: false, keep: false,
    notes: '', salary: '', skills: '', jd_summary: '', jd_ai_summary: '', jd_full: '', discovery_source: '',
    score: 0, score_letter: '', score_status: '', score_reasoning: '', apply_state: '', apply_detail: '',
    cv_status: '', draft_status: '', expires_at: '', followup_sent_at: '', saved_at: '', created_at: '',
    updated_at: '', company_distance_km: 0, company_geo_status: '', ...over,
  }) as Job

test('pay reads đồng as triệu, and leaves other text alone', () => {
  expect(pay('16000000 - 22000000 VND/month', 'vi-VN')).toBe('16–22 triệu/tháng')
  expect(pay('12500000 VND', 'vi-VN')).toBe('12,5 triệu')
  expect(pay('Up to $2000', 'vi-VN')).toBe('Up to $2000')
  expect(payUndisclosed('Thỏa thuận')).toBe(true)
  expect(payUndisclosed('')).toBe(true)
  expect(payUndisclosed('10 - 12 triệu')).toBe(false)
})

test('company names drop the legal form', () => {
  expect(shortCompany('Công ty TNHH Thực phẩm Hoa Sen Vàng')).toBe('Thực phẩm Hoa Sen Vàng')
  expect(shortCompany('Công ty CP Du lịch Sen Việt')).toBe('Du lịch Sen Việt')
  expect(shortCompany('Công ty TNHH MTV Cà phê Mộc')).toBe('Cà phê Mộc')
  expect(shortCompany('Chuỗi cà phê Mộc Lan')).toBe('Chuỗi cà phê Mộc Lan')
})

test('search ignores accents and case, and needs every word', () => {
  const j = job({ title: 'Kế toán tổng hợp', company: 'Đại Lục' })
  expect(matches(j, 'ke toan')).toBe(true)
  expect(matches(j, 'KE dai')).toBe(true)
  expect(matches(j, 'ke kho')).toBe(false)
})

test('pinned first, then best rated, unrated last', () => {
  const list = [
    job({ id: 1, score_status: '', saved_at: '2026-11-07' }),
    job({ id: 2, score_status: 'scored', score: 3.1 }),
    job({ id: 3, score_status: 'scored', score: 4.6 }),
    job({ id: 4, pinned: true }),
  ]
  expect(sortJobs(list).map(j => j.id)).toEqual([4, 3, 2, 1])
})

test('Today puts follow-ups first and shows each job once', () => {
  const jobs = [job({ id: 1 }), job({ id: 2 }), job({ id: 3 })]
  const groups: TodayGroup[] = [
    { kind: 'best_matches', title: '', items: [{ job_id: 1, title: '', why: '' }, { job_id: 2, title: '', why: '' }] },
    { kind: 'deadlines', title: '', items: [{ job_id: 1, title: '', why: '' }] },
    { kind: 'follow_up', title: '', items: [{ job_id: 3, title: '', why: '' }, { job_id: 99, title: '', why: '' }] },
  ]
  expect(todayEntries(groups, jobs).map(e => [e.job.id, e.kind])).toEqual([
    [3, 'follow'],
    [1, 'deadline'],
    [2, 'match'],
  ])
})

test("the server's deal-breaker tags read as words", () => {
  expect(dealbreakerText('Làm T7')).toEqual(['Works on Saturdays'])
  expect(dealbreakerText('Xa 13km')).toEqual(['{n} km away', { n: 13 }])
  expect(dealbreakerText('Something new')).toEqual(['Something new'])
})
