<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useHealthStore } from '../../stores/health'
import { api } from '../../api/client'
import Tag from 'primevue/tag'
import Button from 'primevue/button'

const health = useHealthStore()
const refreshing = ref(false)

const h = computed(() => health.health)

// V2-405: on-demand update check against GitHub releases (server-side, so it
// works without browser CORS and degrades gracefully when airgapped).
interface ComponentVersion {
  current: string
  latest: string
  update_available: boolean
  release_url: string
  checked: boolean
}
interface VersionCheck {
  indelible: ComponentVersion
  antd: ComponentVersion
  github_reachable: boolean
}

const versionInfo = ref<VersionCheck | null>(null)
const checking = ref(false)

// V2-446: data-directory disk usage. The data dir is set at deploy time via
// `data_dir` (TOML) / INDELIBLE_DATA_DIR; this surfaces its live capacity plus,
// when a system-wide storage quota is configured, used-vs-quota.
interface StorageQuota {
  max_bytes: number
  used_bytes: number
  used_pct: number
}
interface StorageInfo {
  data_dir: string
  volume: string
  available: boolean
  total_bytes: number
  used_bytes: number
  free_bytes: number
  used_pct: number
  quota?: StorageQuota | null
}

const storage = ref<StorageInfo | null>(null)

async function fetchStorage() {
  try {
    storage.value = (await api.get('/api/v2/admin/storage')).data
  } catch {
    storage.value = null
  }
}

function formatBytes(n: number | null | undefined): string {
  if (n == null) return '—'
  if (n === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++ }
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${units[i]}`
}

// Colour the used arc by the same thresholds the disk-alert worker uses
// (warning ≥80%, critical ≥95% → uploads pause), so the gauge reads "why".
function usageColor(pct: number): string {
  if (pct >= 95) return '#ef4444' // red-500
  if (pct >= 80) return '#f59e0b' // amber-500
  return '#10b981' // emerald-500
}
const usedColor = computed(() => usageColor(storage.value?.used_pct ?? 0))
// Pie/donut via conic-gradient: the used arc is drawn over a surface-200 track
// circle (transparent remainder lets the track show through), so it adapts to
// dark mode without inline track colours.
const donutStyle = computed(() => {
  const p = Math.max(0, Math.min(100, storage.value?.used_pct ?? 0))
  return { background: `conic-gradient(${usedColor.value} 0 ${p}%, transparent ${p}% 100%)` }
})
const quotaColor = computed(() => usageColor(storage.value?.quota?.used_pct ?? 0))

async function checkVersions() {
  checking.value = true
  try {
    versionInfo.value = (await api.get('/api/v2/admin/version-check')).data
  } catch {
    versionInfo.value = null
  } finally {
    checking.value = false
  }
}

// Render an update badge for a component, or null if the check hasn't run.
function updateTag(c?: ComponentVersion): { label: string; severity: string; url?: string } | null {
  if (!c) return null
  if (!c.checked) return { label: 'Check unavailable', severity: 'secondary' }
  if (c.update_available) return { label: `Update available: ${c.latest}`, severity: 'warn', url: c.release_url }
  return { label: 'Up to date', severity: 'success' }
}

function openUrl(url?: string) {
  if (url) window.open(url, '_blank', 'noopener')
}

const overall = computed(() => {
  if (!health.serverReachable) return { label: 'Unreachable', severity: 'danger' as const }
  switch (h.value?.status) {
    case 'healthy':
      return { label: 'Healthy', severity: 'success' as const }
    case 'degraded':
      return { label: 'Degraded', severity: 'warn' as const }
    case 'unhealthy':
      return { label: 'Unhealthy', severity: 'danger' as const }
    default:
      return { label: 'Unknown', severity: 'secondary' as const }
  }
})

// antd diagnostic fields are present whenever antd is reachable — from managed
// mode's snapshot or a live /health read in the separate-container setup. They
// stay absent only when antd is unreachable or hasn't bootstrapped yet.
const hasAntdDetails = computed(() => h.value?.antd_version != null)

function formatUptime(s?: number): string {
  if (s == null) return '—'
  const d = Math.floor(s / 86400)
  const hrs = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  const parts: string[] = []
  if (d) parts.push(`${d}d`)
  if (hrs) parts.push(`${hrs}h`)
  if (m || (!d && !hrs)) parts.push(`${m}m`)
  return parts.join(' ')
}

async function refresh() {
  refreshing.value = true
  try {
    await Promise.all([health.refresh(), fetchStorage()])
  } finally {
    refreshing.value = false
  }
}

onMounted(refresh)
</script>

<template>
  <div class="p-6">
    <div class="flex items-center justify-between mb-6">
      <div>
        <h1 class="text-2xl font-bold">System Status</h1>
        <p class="text-sm text-surface-400 mt-1">
          Live health of the Indelible service and its Autonomi daemon (antd).
        </p>
      </div>
      <div class="flex gap-2">
        <Button icon="pi pi-github" label="Check for updates" severity="secondary" outlined
          :loading="checking" @click="checkVersions" />
        <Button icon="pi pi-refresh" label="Refresh" :loading="refreshing" @click="refresh" />
      </div>
    </div>

    <p v-if="versionInfo && !versionInfo.github_reachable" class="text-xs text-amber-700 dark:text-amber-400 -mt-3 mb-4">
      Couldn't reach GitHub to check for updates — the host may be offline or firewalled.
    </p>

    <!-- Overall -->
    <div class="bg-surface-0 border border-surface-200 rounded-lg p-5 mb-5 flex items-center gap-4">
      <Tag :value="overall.label" :severity="overall.severity" />
      <span class="text-sm text-surface-500">
        Last checked:
        {{ health.lastChecked ? health.lastChecked.toLocaleTimeString() : '—' }}
      </span>
    </div>

    <!-- antd / network -->
    <div class="bg-surface-0 border border-surface-200 rounded-lg p-5 mb-5">
      <h2 class="text-lg font-semibold mb-4">Autonomi daemon (antd)</h2>

      <!-- The signal the banner is driven by -->
      <div class="flex items-center gap-3 mb-4 p-3 rounded-lg"
        :class="health.antdReachable ? 'bg-green-50 dark:bg-green-950/40' : 'bg-amber-50 dark:bg-amber-950/40'">
        <i class="pi text-lg"
          :class="health.antdReachable
            ? 'pi-check-circle text-green-600 dark:text-green-400'
            : 'pi-exclamation-triangle text-amber-500 dark:text-amber-400'"></i>
        <div>
          <p class="text-sm font-medium"
            :class="health.antdReachable ? 'text-green-700 dark:text-green-300' : 'text-amber-800 dark:text-amber-200'">
            {{ health.antdReachable ? 'Connected to the network' : 'Not reaching the network' }}
          </p>
          <p class="text-xs text-surface-500 mt-0.5">
            <template v-if="health.antdReachable">
              antd is reachable and has peers to quote uploads from.
            </template>
            <template v-else>
              antd is unreachable or has no peers — uploads and downloads will fail
              until it reconnects. Check the antd container/process logs.
            </template>
          </p>
        </div>
      </div>

      <dl class="grid grid-cols-1 sm:grid-cols-2 gap-x-8 gap-y-3 text-sm">
        <div class="flex justify-between gap-4">
          <dt class="text-surface-500">antd URL</dt>
          <dd class="font-mono text-surface-700 truncate">{{ h?.antd_url || '—' }}</dd>
        </div>
        <template v-if="hasAntdDetails">
          <div class="flex justify-between gap-4">
            <dt class="text-surface-500">antd version</dt>
            <dd class="text-surface-700 flex items-center gap-2">
              <span>{{ h?.antd_version }}</span>
              <Tag v-if="updateTag(versionInfo?.antd)" :value="updateTag(versionInfo?.antd)!.label"
                :severity="updateTag(versionInfo?.antd)!.severity"
                :class="updateTag(versionInfo?.antd)!.url ? 'cursor-pointer' : ''"
                @click="openUrl(updateTag(versionInfo?.antd)!.url)" />
            </dd>
          </div>
          <div class="flex justify-between gap-4">
            <dt class="text-surface-500">EVM network</dt>
            <dd class="text-surface-700">{{ h?.antd_evm_network || '—' }}</dd>
          </div>
          <div class="flex justify-between gap-4">
            <dt class="text-surface-500">Uptime</dt>
            <dd class="text-surface-700">{{ formatUptime(h?.antd_uptime_seconds) }}</dd>
          </div>
          <div class="flex justify-between gap-4">
            <dt class="text-surface-500">Build</dt>
            <dd class="font-mono text-surface-700 truncate">{{ h?.antd_build_commit || '—' }}</dd>
          </div>
          <div class="flex justify-between gap-4">
            <dt class="text-surface-500">Payment token</dt>
            <dd class="font-mono text-surface-700 truncate">{{ h?.antd_payment_token_address || '—' }}</dd>
          </div>
          <div class="flex justify-between gap-4">
            <dt class="text-surface-500">Payment vault</dt>
            <dd class="font-mono text-surface-700 truncate">{{ h?.antd_payment_vault_address || '—' }}</dd>
          </div>
        </template>
      </dl>
      <p v-if="!hasAntdDetails" class="text-xs text-surface-400 mt-3">
        Detailed daemon diagnostics (version, EVM network, uptime) appear once antd is reachable.
      </p>
    </div>

    <!-- Service -->
    <div class="bg-surface-0 border border-surface-200 rounded-lg p-5">
      <h2 class="text-lg font-semibold mb-4">Service</h2>
      <dl class="grid grid-cols-1 sm:grid-cols-2 gap-x-8 gap-y-3 text-sm">
        <div class="flex justify-between gap-4">
          <dt class="text-surface-500">Indelible version</dt>
          <dd class="text-surface-700 flex items-center gap-2">
            <span>{{ h?.version || '—' }}</span>
            <Tag v-if="updateTag(versionInfo?.indelible)" :value="updateTag(versionInfo?.indelible)!.label"
              :severity="updateTag(versionInfo?.indelible)!.severity"
              :class="updateTag(versionInfo?.indelible)!.url ? 'cursor-pointer' : ''"
              @click="openUrl(updateTag(versionInfo?.indelible)!.url)" />
          </dd>
        </div>
        <div class="flex justify-between gap-4">
          <dt class="text-surface-500">Database</dt>
          <dd>
            <Tag :value="h?.database === false ? 'Down' : 'Connected'"
              :severity="h?.database === false ? 'danger' : 'success'" />
          </dd>
        </div>
        <div class="flex justify-between gap-4">
          <dt class="text-surface-500">Upload queue</dt>
          <dd class="text-surface-700">{{ h?.queued ?? 0 }} queued · {{ h?.processing ?? 0 }} processing</dd>
        </div>
        <div class="flex justify-between gap-4">
          <dt class="text-surface-500">Notifier</dt>
          <dd class="text-surface-700">{{ h?.notifier || '—' }}</dd>
        </div>
      </dl>
    </div>

    <!-- Storage / data-directory capacity -->
    <div class="bg-surface-0 border border-surface-200 rounded-lg p-5 mt-5">
      <h2 class="text-lg font-semibold mb-4">Storage</h2>

      <div v-if="storage?.available" class="flex flex-col sm:flex-row items-center gap-8">
        <!-- Capacity pie/donut -->
        <div class="relative w-36 h-36 shrink-0 rounded-full bg-surface-200">
          <div class="absolute inset-0 rounded-full" :style="donutStyle"></div>
          <div class="absolute inset-[20%] rounded-full bg-surface-0 flex flex-col items-center justify-center">
            <span class="text-2xl font-bold leading-none">{{ storage.used_pct.toFixed(0) }}%</span>
            <span class="text-xs text-surface-400 mt-1">used</span>
          </div>
        </div>

        <!-- Figures + location -->
        <div class="flex-1 w-full">
          <dl class="grid grid-cols-1 sm:grid-cols-2 gap-x-8 gap-y-3 text-sm">
            <div class="flex items-center justify-between gap-4">
              <dt class="flex items-center gap-2 text-surface-500">
                <span class="inline-block w-3 h-3 rounded-sm" :style="{ background: usedColor }"></span>Used
              </dt>
              <dd class="text-surface-700">{{ formatBytes(storage.used_bytes) }}</dd>
            </div>
            <div class="flex items-center justify-between gap-4">
              <dt class="flex items-center gap-2 text-surface-500">
                <span class="inline-block w-3 h-3 rounded-sm bg-surface-300"></span>Free
              </dt>
              <dd class="text-surface-700">{{ formatBytes(storage.free_bytes) }}</dd>
            </div>
            <div class="flex justify-between gap-4">
              <dt class="text-surface-500">Total capacity</dt>
              <dd class="text-surface-700">{{ formatBytes(storage.total_bytes) }}</dd>
            </div>
            <div class="flex justify-between gap-4">
              <dt class="text-surface-500">Drive</dt>
              <dd class="font-mono text-surface-700">{{ storage.volume || 'root (/)' }}</dd>
            </div>
            <div class="flex justify-between gap-4 sm:col-span-2">
              <dt class="text-surface-500 shrink-0">Data directory</dt>
              <dd class="font-mono text-surface-700 truncate" v-tooltip.top="storage.data_dir">{{ storage.data_dir }}</dd>
            </div>
          </dl>

          <!-- System storage quota (only when one is configured) -->
          <div v-if="storage.quota" class="mt-4 pt-4 border-t border-surface-200">
            <div class="flex justify-between text-sm mb-1.5">
              <span class="text-surface-500">System quota</span>
              <span class="text-surface-700">
                {{ formatBytes(storage.quota.used_bytes) }} / {{ formatBytes(storage.quota.max_bytes) }}
                ({{ storage.quota.used_pct.toFixed(0) }}%)
              </span>
            </div>
            <div class="w-full h-2 rounded-full bg-surface-200 overflow-hidden">
              <div class="h-full rounded-full transition-all"
                :style="{ width: Math.min(100, storage.quota.used_pct) + '%', background: quotaColor }"></div>
            </div>
          </div>
        </div>
      </div>

      <p v-else class="text-sm text-surface-400">
        Disk usage is unavailable — the data directory's filesystem stats couldn't be read on this host.
      </p>
    </div>
  </div>
</template>
