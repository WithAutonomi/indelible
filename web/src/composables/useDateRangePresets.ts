// Shared date-range presets for the Logs (V2-409) and Uploads (V2-410) filters.
// Kept framework-free and pure so the date math is unit-tested in one place
// rather than duplicated per view.

export type DatePreset = '' | '24h' | 'yesterday' | 'lastweek' | '7d' | '14d'

export const PRESET_OPTIONS: Array<{ label: string; value: DatePreset }> = [
  { label: 'Custom', value: '' },
  { label: 'Last 24h', value: '24h' },
  { label: 'Yesterday', value: 'yesterday' },
  { label: 'Last week', value: 'lastweek' },
  { label: 'Last 7 days', value: '7d' },
  { label: 'Last 14 days', value: '14d' },
]

const DAY = 86_400_000

/**
 * Compute an exact [since, until] window for a preset, relative to `now` in the
 * viewer's local time. Returns null for '' (Custom).
 *
 * Rolling presets (24h / 7d / 14d) end at `now`; calendar presets (yesterday,
 * last week Mon–Sun) span whole local days. Callers send these as RFC3339
 * timestamps so the window is exact rather than snapped to day boundaries — the
 * backend accepts both RFC3339 and YYYY-MM-DD.
 */
export function presetRange(
  preset: DatePreset,
  now: Date = new Date(),
): { since: Date; until: Date } | null {
  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  switch (preset) {
    case '24h':
      return { since: new Date(now.getTime() - DAY), until: now }
    case '7d':
      return { since: new Date(now.getTime() - 7 * DAY), until: now }
    case '14d':
      return { since: new Date(now.getTime() - 14 * DAY), until: now }
    case 'yesterday':
      return {
        since: new Date(startOfToday.getTime() - DAY),
        until: new Date(startOfToday.getTime() - 1),
      }
    case 'lastweek': {
      // getDay(): 0=Sun..6=Sat. Days elapsed since this week's Monday:
      const sinceMonday = (startOfToday.getDay() + 6) % 7
      const thisMonday = new Date(startOfToday.getTime() - sinceMonday * DAY)
      return {
        since: new Date(thisMonday.getTime() - 7 * DAY),
        until: new Date(thisMonday.getTime() - 1),
      }
    }
    default:
      return null
  }
}
