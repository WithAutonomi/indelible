import { describe, it, expect } from 'vitest'
import { presetRange } from '../useDateRangePresets'

// Tuesday 2 June 2026, 14:30 local. Assertions use local date components so the
// test is timezone-independent (presetRange works in local time).
const NOW = new Date(2026, 5, 2, 14, 30, 0)

describe('presetRange', () => {
  it('returns null for Custom', () => {
    expect(presetRange('', NOW)).toBeNull()
  })

  it('last 24h is a rolling window ending at now', () => {
    const r = presetRange('24h', NOW)!
    expect(r.until).toBe(NOW)
    expect([r.since.getMonth(), r.since.getDate(), r.since.getHours(), r.since.getMinutes()])
      .toEqual([5, 1, 14, 30]) // 1 June 14:30
  })

  it('last 7 / 14 days roll back from now', () => {
    const r7 = presetRange('7d', NOW)!
    expect([r7.since.getMonth(), r7.since.getDate(), r7.since.getHours()]).toEqual([4, 26, 14]) // 26 May 14:30
    const r14 = presetRange('14d', NOW)!
    expect([r14.since.getMonth(), r14.since.getDate(), r14.since.getHours()]).toEqual([4, 19, 14]) // 19 May 14:30
  })

  it('yesterday spans the previous whole local day', () => {
    const r = presetRange('yesterday', NOW)!
    expect([r.since.getMonth(), r.since.getDate(), r.since.getHours(), r.since.getMinutes()])
      .toEqual([5, 1, 0, 0]) // 1 June 00:00
    expect([r.until.getMonth(), r.until.getDate(), r.until.getHours(), r.until.getMinutes(), r.until.getSeconds()])
      .toEqual([5, 1, 23, 59, 59]) // 1 June 23:59:59(.999)
  })

  it('last week is the previous Monday–Sunday', () => {
    const r = presetRange('lastweek', NOW)!
    // NOW is a Tuesday; this week's Monday is 1 June, so last week is 25–31 May.
    expect([r.since.getMonth(), r.since.getDate(), r.since.getHours()]).toEqual([4, 25, 0]) // Mon 25 May 00:00
    expect([r.until.getMonth(), r.until.getDate(), r.until.getHours(), r.until.getMinutes()])
      .toEqual([4, 31, 23, 59]) // Sun 31 May 23:59
  })
})
