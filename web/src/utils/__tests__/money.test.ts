import { describe, it, expect } from 'vitest'
import {
  attoToANT,
  attoToUSDCents,
  fmtUSD,
  approxUSD,
  grossEstimateAtto,
  fmtFeePerBatch,
  gbRemaining,
  costPerGBTooltip,
  type CostPerGBBasis,
} from '../money'

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

  it('gbRemaining floors to one decimal with exact BigInt math (V2-1114)', () => {
    // exact division: 10 ANT balance / 0.032 ANT per GB = 312.5 GB
    expect(gbRemaining('10000000000000000000', '32000000000000000')).toBe('312.5')
    // floor, never round up: 1 / 0.3 = 3.333… → 3.3
    expect(gbRemaining('1000000000000000000', '300000000000000000')).toBe('3.3')
    // 0.29999… would round to 0.3 — floor says 0.2
    expect(gbRemaining('299999999999999999', '1000000000000000000')).toBe('0.2')
    // below a tenth of a GB: honest 0.0 (the line still shows — near-empty)
    expect(gbRemaining('1', '32000000000000000')).toBe('0.0')
    // zero balance is a real answer, not a hidden line
    expect(gbRemaining('0', '32000000000000000')).toBe('0.0')
  })

  it('gbRemaining hides (null) when the estimate is absent or unusable', () => {
    // absent estimate — older gateway or "not enough data yet"
    expect(gbRemaining('10000000000000000000', '')).toBeNull()
    expect(gbRemaining('10000000000000000000', null)).toBeNull()
    expect(gbRemaining('10000000000000000000', undefined)).toBeNull()
    // absent balance (gateway unreachable)
    expect(gbRemaining(null, '32000000000000000')).toBeNull()
    // junk and non-positive costs never divide
    expect(gbRemaining('10', 'junk')).toBeNull()
    expect(gbRemaining('junk', '10')).toBeNull()
    expect(gbRemaining('10', '0')).toBeNull()
    expect(gbRemaining('10', '-5')).toBeNull()
    expect(gbRemaining('-10', '5')).toBeNull()
  })

  it('costPerGBTooltip renders the gateway basis, hides without one', () => {
    const basis: CostPerGBBasis = {
      median_paid_per_quote_atto: '105468750000000',
      sample_quotes: 128,
      window: '7d',
      chunks_per_gb: 256,
      batches_per_gb: 1,
    }
    const tip = costPerGBTooltip(basis)!
    // methodology from the basis object, nothing hardcoded
    expect(tip).toContain('0.00010546875 ANT') // the median, exact attoToANT form
    expect(tip).toContain('256 chunks/GB')
    expect(tip).toContain('1 × network fee')
    expect(tip).toContain('128 paid quotes')
    expect(tip).toContain('last 7d')
    // the honesty clause the ticket requires
    expect(tip).toContain('deduplication and market movement')
    // no fee term when the gateway charges per zero batches
    expect(costPerGBTooltip({ ...basis, batches_per_gb: 0 })).not.toContain('network fee')
    // no basis → no tooltip (never a made-up methodology)
    expect(costPerGBTooltip(null)).toBeNull()
    expect(costPerGBTooltip(undefined)).toBeNull()
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
