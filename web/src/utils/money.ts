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
