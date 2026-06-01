<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useHealthStore } from '../../stores/health'
import Tag from 'primevue/tag'
import Button from 'primevue/button'

const health = useHealthStore()
const refreshing = ref(false)

const h = computed(() => health.health)

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

// antd diagnostic fields are only present when antd is managed; in the default
// separate-container compose setup they're absent and only `antd`/`antd_url`
// are reported.
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
      <Button icon="pi pi-refresh" label="Refresh" :loading="refreshing" @click="refresh" />
    </div>

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
        :class="health.antdReachable ? 'bg-green-50' : 'bg-amber-50'">
        <i class="pi text-lg"
          :class="health.antdReachable
            ? 'pi-check-circle text-green-600'
            : 'pi-exclamation-triangle text-amber-500'"></i>
        <div>
          <p class="text-sm font-medium"
            :class="health.antdReachable ? 'text-green-700' : 'text-amber-800'">
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
            <dd class="text-surface-700">{{ h?.antd_version }}</dd>
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
        Detailed daemon diagnostics (version, EVM network, uptime) are reported only when
        antd runs in managed mode.
      </p>
    </div>

    <!-- Service -->
    <div class="bg-surface-0 border border-surface-200 rounded-lg p-5">
      <h2 class="text-lg font-semibold mb-4">Service</h2>
      <dl class="grid grid-cols-1 sm:grid-cols-2 gap-x-8 gap-y-3 text-sm">
        <div class="flex justify-between gap-4">
          <dt class="text-surface-500">Indelible version</dt>
          <dd class="text-surface-700">{{ h?.version || '—' }}</dd>
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
