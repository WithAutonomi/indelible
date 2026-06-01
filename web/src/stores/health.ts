import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { api } from '../api/client'

// Mirrors the unauthenticated GET /health payload. The antd_* fields are only
// present when antd is managed and has reported at least once, so they're all
// optional.
export interface HealthStatus {
  status: 'healthy' | 'degraded' | 'unhealthy'
  version?: string
  database?: boolean
  antd?: boolean
  antd_url?: string
  queued?: number
  processing?: number
  notifier?: string
  antd_version?: string
  antd_evm_network?: string
  antd_uptime_seconds?: number
  antd_build_commit?: string
  antd_payment_token_address?: string
  antd_payment_vault_address?: string
}

export const useHealthStore = defineStore('health', () => {
  const health = ref<HealthStatus | null>(null)
  // Did the /health request itself succeed? false => the indelible backend is
  // unreachable, which is a bigger problem than antd merely being down.
  const serverReachable = ref(true)
  const lastChecked = ref<Date | null>(null)
  let timer: ReturnType<typeof setInterval> | null = null

  // antd reached the network on the last probe. The backend's /health runs a
  // real DataCost probe, so `true` means "reachable AND has peers to quote",
  // not just "process alive".
  const antdReachable = computed(() => health.value?.antd === true)

  // The condition worth alerting admins about: antd can't serve the network
  // (or we can't even reach the backend to find out).
  const networkDegraded = computed(
    () => !serverReachable.value || health.value?.antd === false,
  )

  async function refresh() {
    try {
      const res = await api.get('/health', { _skipAuthRedirect: true } as any)
      health.value = res.data
      serverReachable.value = true
    } catch (e: unknown) {
      // A 503 still carries a JSON body (e.g. DB down) — keep it so the panel
      // can show what's wrong. Anything without a response body means the
      // backend itself is unreachable.
      const resp = (e as { response?: { data?: unknown } })?.response
      if (resp?.data && typeof resp.data === 'object') {
        health.value = resp.data as HealthStatus
        serverReachable.value = true
      } else {
        serverReachable.value = false
      }
    } finally {
      lastChecked.value = new Date()
    }
  }

  // Poll on an interval. Idempotent — calling start() while already polling is
  // a no-op, so AppLayout can drive it from a watcher without double-timers.
  function start(intervalMs = 30000) {
    if (timer !== null) return
    void refresh()
    timer = setInterval(() => void refresh(), intervalMs)
  }

  function stop() {
    if (timer !== null) {
      clearInterval(timer)
      timer = null
    }
  }

  return {
    health,
    serverReachable,
    lastChecked,
    antdReachable,
    networkDegraded,
    refresh,
    start,
    stop,
  }
})
