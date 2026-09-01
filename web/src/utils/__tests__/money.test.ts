import { describe, it, expect } from 'vitest'
import { attoToANT, attoToUSDCents, fmtUSD, approxUSD, grossEstimateAtto, fmtFeePerBatch } from '../money'

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

describe('fee-aware estimates (V2-1113)', () => {
  it('grossEstimateAtto adds fee × batches with exact BigInt math', () => {
    // the rig's standard batch + its per-batch fee — gross to the atto
    expect(grossEstimateAtto('35156250000000000', '5000000000000000')).toBe('40156250000000000')
    expect(grossEstimateAtto('35156250000000000', '5000000000000000', 3)).toBe('50156250000000000')
    // no/zero/unknown fee → the net estimate unchanged
    expect(grossEstimateAtto('12345', null)).toBe('12345')
    expect(grossEstimateAtto('12345', '0')).toBe('12345')
    expect(grossEstimateAtto('12345', 'junk')).toBe('12345')
    // zero batches (full dedup sends no batch) pays no fee
    expect(grossEstimateAtto('0', '5000000000000000', 0)).toBe('0')
    // unusable cost passes through, matching attoToANT's tolerance
    expect(grossEstimateAtto('junk', '5')).toBe('junk')
  })

  it('fmtFeePerBatch prefers fiat, falls back to ANT, hides when no fee', () => {
    // 0.005 ANT at $0.35 = $0.00175 → nearest cent ≈ $0.00; use a fee that rounds visibly
    expect(fmtFeePerBatch('50000000000000000', '0.35')).toBe('≈ $0.02')
    expect(fmtFeePerBatch('5000000000000000', '')).toBe('0.005 ANT')
    expect(fmtFeePerBatch('5000000000000000', null)).toBe('0.005 ANT')
    expect(fmtFeePerBatch('0', '0.35')).toBeNull()
    expect(fmtFeePerBatch('', '0.35')).toBeNull()
    expect(fmtFeePerBatch(null, '0.35')).toBeNull()
    expect(fmtFeePerBatch('junk', '0.35')).toBeNull()
  })
})
