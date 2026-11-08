import { afterEach, expect, test, vi } from 'vitest'
import { gatherer } from './live'

afterEach(() => vi.useRealTimers())

test('a burst of changes is one refetch', () => {
  vi.useFakeTimers()
  const refresh = vi.fn()
  const g = gatherer(refresh, () => false, 300)
  for (let i = 0; i < 20; i++) g.changed()
  vi.advanceTimersByTime(299)
  expect(refresh).not.toHaveBeenCalled()
  vi.advanceTimersByTime(1)
  expect(refresh).toHaveBeenCalledTimes(1)
  g.changed()
  vi.advanceTimersByTime(300)
  expect(refresh).toHaveBeenCalledTimes(2)
})

test('a hidden tab waits until it is looked at, then refetches once', () => {
  vi.useFakeTimers()
  const refresh = vi.fn()
  let hidden = true
  const g = gatherer(refresh, () => hidden, 300)
  g.changed()
  vi.advanceTimersByTime(300)
  g.changed()
  vi.advanceTimersByTime(300)
  expect(refresh).not.toHaveBeenCalled()
  g.shown() // still hidden: nothing
  expect(refresh).not.toHaveBeenCalled()
  hidden = false
  g.shown()
  g.shown()
  expect(refresh).toHaveBeenCalledTimes(1)
})

test('shown with nothing missed does nothing', () => {
  const refresh = vi.fn()
  gatherer(refresh, () => false).shown()
  expect(refresh).not.toHaveBeenCalled()
})
