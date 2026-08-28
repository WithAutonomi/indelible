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
