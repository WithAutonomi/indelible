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
    await health.refresh()
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
  </div>
</template>
