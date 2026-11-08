import { expect, test } from 'vitest'
import type { Job, TodayGroup } from './api'
import { dealbreakerText, matches, payTop, postingUrl, pay, registerUrl, payUndisclosed, shortCompany, sortJobs, steps, todayEntries } from './jobs'

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

test('only web links are links', () => {
  expect(postingUrl(job({ url: ' https://topcv.vn/x ' }))).toBe('https://topcv.vn/x')
  expect(postingUrl(job({ url: 'javascript:alert(1)' }))).toBe('')
  expect(postingUrl(job({ url: 'data:text/html,hi' }))).toBe('')
})

test('the top of the pay reads in millions', () => {
  expect(payTop('16000000 - 22000000 VND/month')).toBe(22)
  expect(payTop('15-20 triệu')).toBe(20)
  expect(payTop('Up to 12,5 triệu')).toBe(12.5)
  expect(payTop('Thỏa thuận')).toBeNull()
  expect(payTop('$1,500')).toBeNull()
})

test('lists sort by fit, pay, distance and deadline; pinned first, unknowns last', () => {
  const a = job({ id: 1, score_status: 'scored', score: 4.6, salary: '10 triệu', company_geo_status: 'ok', company_distance_km: 9 })
  const b = job({ id: 2, score_status: 'scored', score: 3.1, salary: '20 triệu', clock: { kind: 'deadline', state: 'warning', days: 2, date: '' } })
  const c = job({ id: 3, salary: 'Thỏa thuận', company_geo_status: 'ok', company_distance_km: 2 })
  const p = job({ id: 4, pinned: true })
  const ids = (k: Parameters<typeof sortJobs>[1]) => sortJobs([a, b, c, p], k).map(j => j.id)
  expect(ids('fit')).toEqual([4, 1, 2, 3])
  expect(ids('pay')).toEqual([4, 2, 1, 3])
  expect(ids('near')).toEqual([4, 3, 1, 2])
  expect(ids('deadline')).toEqual([4, 2, 1, 3])
})

test("a job's steps keep only the dates braveduck knows", () => {
  const j = job({ status: 'interview', saved_at: '2026-10-20T08:00:00Z', expires_at: 'applied:2026-10-30', followup_sent_at: '2026-11-07T09:00:00Z' })
  expect(steps(j)).toEqual([
    { stage: 'saved', date: '2026-10-20T08:00:00Z' },
    { stage: 'applied', date: '2026-10-30' },
    { stage: 'followup', date: '2026-11-07T09:00:00Z' },
    { stage: 'interview', date: '' },
  ])
  expect(steps(job({ status: 'saved', saved_at: '2026-11-01' }))).toEqual([{ stage: 'saved', date: '2026-11-01' }])
})

test("only the business register's own pages become a company link", () => {
  const page = 'https://dulieuphapluat.vn/cong-cu/ma-so-thue/0309542960-x.html'
  expect(registerUrl(job({ company_info_url: page }))).toBe(page)
  expect(registerUrl(job({ company_info_url: 'https://dulieuphapluat.vn.evil.example/x' }))).toBe('')
  expect(registerUrl(job({ company_info_url: 'javascript:alert(1)' }))).toBe('')
  expect(registerUrl(job({ company_info_url: '' }))).toBe('')
  expect(dealbreakerText('Cty ngừng hoạt động')).toEqual(['Company has stopped operating'])
})
