import { describe, it, expect } from 'vitest'
import { attoToANT, attoToUSDCents, fmtUSD, approxUSD } from '../money'

describe('money (V2-1100 crypto-free display)', () => {
  it('attoToANT full precision', () => {
    expect(attoToANT('35156250000000000')).toBe('0.03515625')
    expect(attoToANT('71358258928571428571')).toBe('71.358258928571428571')
    expect(attoToANT('1000000000000000000')).toBe('1')
    expect(attoToANT('0')).toBe('0')
    expect(attoToANT('junk')).toBe('junk')
  })

  it('attoToUSDCents rounds to nearest cent at an exact rational rate', () => {
    // 1 ANT at $0.35 = 35 cents exactly
    expect(attoToUSDCents('1000000000000000000', '0.35')).toBe(35n)
    // the rig's standard batch: 0.03515625 ANT × 0.35 = $0.0123… → 1 cent
    expect(attoToUSDCents('35156250000000000', '0.35')).toBe(1n)
    // $25 stripe credit at 0.35: 71.428571428571428571 ANT → exactly $25.00
    expect(attoToUSDCents('71428571428571428571', '0.35')).toBe(2500n)
    expect(attoToUSDCents('x', '0.35')).toBeNull()
    expect(attoToUSDCents('1', 'not-a-rate')).toBeNull()
    expect(attoToUSDCents('1', '0')).toBeNull()
  })

  it('fmtUSD renders cents', () => {
    expect(fmtUSD(2500n)).toBe('$25.00')
    expect(fmtUSD(1)).toBe('$0.01')
    expect(fmtUSD(-150n)).toBe('-$1.50')
  })

  it('approxUSD is the one-call display path with honest fallback', () => {
    expect(approxUSD('71428571428571428571', '0.35')).toBe('≈ $25.00')
    expect(approxUSD('1000000000000000000', '')).toBeNull()
    expect(approxUSD(null, '0.35')).toBeNull()
  })
})
