// Crypto-free display helpers (V2-1100): exact BigInt conversions from the
// ANT-denominated ledger to fiat at the gateway's USD-per-ANT rate. The
// ledger stays ANT underneath (crypto-lite); fiat is a display conversion,
// labelled approximate because the rate can move after purchase. No floats
// anywhere near money.

const ATTO = 10n ** 18n

/** Atto → decimal ANT string, full precision, trailing zeros trimmed. */
export function attoToANT(atto: string): string {
  let v: bigint
  try {
    v = BigInt(atto)
  } catch {
    return atto
  }
  const neg = v < 0n ? '-' : ''
  if (v < 0n) v = -v
  const whole = v / ATTO
  const frac = (v % ATTO).toString().padStart(18, '0').replace(/0+$/, '')
  return frac ? `${neg}${whole}.${frac}` : `${neg}${whole}`
}

/** Parse a decimal rate string ("0.35") into an exact rational. */
function parseRate(rate: string): { num: bigint; denom: bigint } | null {
  const m = /^(\d+)(?:\.(\d{1,18}))?$/.exec(rate.trim())
  if (!m) return null
  const frac = m[2] ?? ''
  const num = BigInt(m[1] + frac)
  if (num <= 0n) return null
  return { num, denom: 10n ** BigInt(frac.length) }
}

/**
 * Atto-ANT → USD cents at the given rate, rounded to the nearest cent.
 * Returns null when either input is unusable — callers fall back to ANT.
 */
export function attoToUSDCents(atto: string, rate: string): bigint | null {
  const r = parseRate(rate)
  if (!r) return null
  let v: bigint
  try {
    v = BigInt(atto)
  } catch {
    return null
  }
  const neg = v < 0n
  if (neg) v = -v
  const denom = r.denom * ATTO
  const cents = (v * r.num * 100n * 2n + denom) / (2n * denom)
  return neg ? -cents : cents
}

/** Cents → "$12.34" (or "-$12.34"). */
export function fmtUSD(cents: bigint | number): string {
  let v = BigInt(cents)
  const neg = v < 0n ? '-' : ''
  if (v < 0n) v = -v
  return `${neg}$${v / 100n}.${(v % 100n).toString().padStart(2, '0')}`
}

/**
 * The one-call display helper: "≈ $1.23" for an atto amount at a rate, or
 * null when conversion isn't possible (no rate) — caller shows ANT instead.
 * The ≈ is honest: display conversion at the current rate, not a ledger fact.
 */
export function approxUSD(atto: string | null | undefined, rate: string | null | undefined): string | null {
  if (!atto || !rate) return null
  const cents = attoToUSDCents(atto, rate)
  if (cents === null) return null
  return `≈ ${fmtUSD(cents)}`
}

/**
 * Gross pre-upload estimate (V2-1113): batch total + fee × batches, exact
 * BigInt atto in/out — the basis the gateway actually debits (V2-1098).
 * Unusable cost passes through unchanged; unusable/zero fee adds nothing.
 */
export function grossEstimateAtto(
  costAtto: string,
  feeAtto: string | null | undefined,
  batches: number = 1,
): string {
  let cost: bigint
  try {
    cost = BigInt(costAtto)
  } catch {
    return costAtto
  }
  let fee = 0n
  try {
    fee = BigInt(feeAtto || '0')
  } catch {
    fee = 0n
  }
  if (fee <= 0n || batches <= 0) return cost.toString()
  return (cost + fee * BigInt(batches)).toString()
}

/**
 * The gateway's cost-per-GB estimate basis (V2-1114), relayed verbatim so
 * this UI hardcodes no methodology numbers — the tooltip renders whatever
 * the gateway actually computed with.
 */
export type CostPerGBBasis = {
  median_paid_per_quote_atto: string
  sample_quotes: number
  window: string
  chunks_per_gb: number
  batches_per_gb: number
}

/**
 * "≈ N GB remaining at current prices" (V2-1114): balance ÷ cost-per-GB to
 * one decimal, FLOORED — a capacity line must never promise more than the
 * credits cover. Exact BigInt math (tenths = balance × 10 / cost). Null when
 * either input is missing, unusable, or non-positive — the caller hides the
 * line entirely (absent means "not enough data yet", never zero GB).
 */
export function gbRemaining(
  balanceAtto: string | null | undefined,
  costPerGBAtto: string | null | undefined,
): string | null {
  if (!balanceAtto || !costPerGBAtto) return null
  let bal: bigint, cost: bigint
  try {
    bal = BigInt(balanceAtto)
    cost = BigInt(costPerGBAtto)
  } catch {
    return null
  }
  if (cost <= 0n || bal < 0n) return null
  const tenths = (bal * 10n) / cost
  return `${tenths / 10n}.${tenths % 10n}`
}

/**
 * Methodology tooltip for the capacity line (V2-1114), built entirely from
 * the gateway's basis object. Null without a usable basis — the line then
 * renders with no tooltip rather than a made-up methodology.
 */
export function costPerGBTooltip(basis: CostPerGBBasis | null | undefined): string | null {
  if (!basis || !basis.median_paid_per_quote_atto || !basis.chunks_per_gb) return null
  const feeTerm = basis.batches_per_gb > 0 ? ` + ${basis.batches_per_gb} × network fee` : ''
  return (
    `Estimated from the gateway's recent payments: median chunk price ` +
    `${attoToANT(basis.median_paid_per_quote_atto)} ANT × ${basis.chunks_per_gb} chunks/GB${feeTerm}, ` +
    `over ${basis.sample_quotes} paid quotes in the last ${basis.window}. ` +
    `Actual cost varies with deduplication and market movement.`
  )
}

/**
 * Display form of the gateway's per-batch network fee (V2-1113): "≈ $0.01"
 * at a rate, "0.005 ANT" without one, null when the fee is unknown or zero —
 * caller hides the note. Marked per batch by the caller; every upload settles
 * as one gateway batch, so per upload reads the same.
 */
export function fmtFeePerBatch(
  feeAtto: string | null | undefined,
  rate: string | null | undefined,
): string | null {
  if (!feeAtto) return null
  let v: bigint
  try {
    v = BigInt(feeAtto)
  } catch {
    return null
  }
  if (v <= 0n) return null
  return approxUSD(feeAtto, rate) ?? `${attoToANT(feeAtto)} ANT`
}
