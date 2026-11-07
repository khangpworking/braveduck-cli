import { expect, test } from 'vitest'
import type { Job } from './api'
import { applySite, draftOf, mailto, splitEmail, type Platform } from './jobApi'

const job = (over: Partial<Job>) => ({ url: '', draft_email: '', ...over }) as Job

test('an email draft reads from its JSON, and a broken one is no draft', () => {
  expect(draftOf(job({ draft_email: '{"to":"hr@x.vn","subject":"Hi","body":"Hello"}'}))).toEqual({ to: 'hr@x.vn', subject: 'Hi', body: 'Hello' })
  expect(draftOf(job({ draft_email: '' }))).toBeNull()
  expect(draftOf(job({ draft_email: '{oops' }))).toBeNull()
})

test('a pasted email splits into subject and body', () => {
  expect(splitEmail('Subject: Ứng tuyển kế toán\n\nKính gửi anh chị')).toEqual({ subject: 'Ứng tuyển kế toán', body: 'Kính gửi anh chị' })
  expect(splitEmail('Just a body')).toEqual({ subject: '', body: 'Just a body' })
})

test('mailto keeps spaces as %20 and the address readable', () => {
  expect(mailto({ to: 'hr@x.vn', subject: 'Kế toán kho', body: 'a b' })).toBe(
    'mailto:hr@x.vn?subject=K%E1%BA%BF%20to%C3%A1n%20kho&body=a%20b',
  )
})

test('assisted apply needs a connected site that owns the job link', () => {
  const p = (status: Platform['status']): Platform => ({ id: 'topcv', name: 'TopCV', status, domains: ['topcv.vn'], verified: true, builtin: true })
  const j = job({ url: 'https://www.topcv.vn/viec-lam/x/1.html' })
  expect(applySite(j, [p('connected')])?.name).toBe('TopCV')
  expect(applySite(j, [p('expired')])).toBeUndefined()
  expect(applySite(job({ url: 'https://evil-topcv.vn/x' }), [p('connected')])).toBeUndefined()
  expect(applySite(job({ url: 'not a link' }), [p('connected')])).toBeUndefined()
})
