<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '../../api/client'
import Select from 'primevue/select'
import Card from 'primevue/card'

const days = ref(7)
const uploadStats = ref<any>(null)
const tokenStats = ref<any>(null)
const costStats = ref<any>(null)
const loading = ref(true)

const periodOptions = [
  { label: 'Last 7 days', value: 7 },
  { label: 'Last 30 days', value: 30 },
  { label: 'Last 90 days', value: 90 },
]

async function fetchAll() {
  loading.value = true
  try {
    const [uploads, tokens, costs] = await Promise.all([
      api.get('/api/v2/admin/analytics/uploads', { params: { days: days.value } }),
      api.get('/api/v2/admin/analytics/tokens', { params: { days: days.value } }),
      api.get('/api/v2/admin/analytics/costs', { params: { days: days.value } }),
    ])
    uploadStats.value = uploads.data
    tokenStats.value = tokens.data
    costStats.value = costs.data
  } catch {
    // ignore
  } finally {
    loading.value = false
  }
}

function formatBytes(bytes: number) {
  if (!bytes) return '0 B'
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB'
  if (bytes < 1073741824) return (bytes / 1048576).toFixed(1) + ' MB'
  return (bytes / 1073741824).toFixed(2) + ' GB'
}

onMounted(fetchAll)
</script>

<template>
  <div class="p-6">
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold">Analytics</h1>
      <div class="flex items-center gap-2">
        <label class="text-sm text-surface-500">Period:</label>
        <Select v-model="days" :options="periodOptions" optionLabel="label" optionValue="value"
          @update:modelValue="fetchAll" class="w-44" />
      </div>
    </div>

    <div v-if="loading" class="text-center text-surface-400 py-12">Loading analytics...</div>

    <template v-else>
      <!-- Upload stats -->
      <div class="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
        <Card>
          <template #content>
            <p class="text-xs text-surface-500 uppercase">Completed</p>
            <p class="text-2xl font-bold text-green-600">{{ uploadStats?.status_counts?.completed || 0 }}</p>
          </template>
        </Card>
        <Card>
          <template #content>
            <p class="text-xs text-surface-500 uppercase">Queued</p>
            <p class="text-2xl font-bold text-yellow-600">{{ uploadStats?.status_counts?.queued || 0 }}</p>
          </template>
        </Card>
        <Card>
          <template #content>
            <p class="text-xs text-surface-500 uppercase">Failed</p>
            <p class="text-2xl font-bold text-red-600">{{ uploadStats?.status_counts?.failed || 0 }}</p>
          </template>
        </Card>
        <Card>
          <template #content>
            <p class="text-xs text-surface-500 uppercase">Avg Size</p>
            <p class="text-2xl font-bold">{{ formatBytes(uploadStats?.avg_file_size || 0) }}</p>
          </template>
        </Card>
      </div>

      <!-- Token stats -->
      <Card class="mb-6">
        <template #title>API Token Usage</template>
        <template #content>
          <div class="grid grid-cols-2 gap-4 mb-4">
            <div>
              <p class="text-xs text-surface-500 uppercase">Total Requests</p>
              <p class="text-2xl font-bold">{{ tokenStats?.total_requests || 0 }}</p>
            </div>
            <div>
              <p class="text-xs text-surface-500 uppercase">Active Tokens</p>
              <p class="text-2xl font-bold">{{ tokenStats?.active_tokens || 0 }}</p>
            </div>
          </div>
          <div v-if="tokenStats?.top_tokens?.length">
            <h3 class="text-sm font-medium mb-2">Top Tokens</h3>
            <div class="space-y-1">
              <div v-for="t in tokenStats.top_tokens" :key="t.token_uuid" class="flex justify-between text-sm">
                <span>{{ t.token_name }}</span>
                <span class="text-surface-500">{{ t.requests }} requests</span>
              </div>
            </div>
          </div>
        </template>
      </Card>

      <!-- Cost stats -->
      <Card>
        <template #title>Storage Costs</template>
        <template #content>
          <div class="grid grid-cols-3 gap-4 mb-4">
            <div>
              <p class="text-xs text-surface-500 uppercase">Total Uploads</p>
              <p class="text-2xl font-bold">{{ costStats?.total_uploads || 0 }}</p>
            </div>
            <div>
              <p class="text-xs text-surface-500 uppercase">Total Spent (atto)</p>
              <p class="text-2xl font-bold font-mono">{{ costStats?.total_cost || '0' }}</p>
            </div>
            <div>
              <p class="text-xs text-surface-500 uppercase">Avg / Upload (atto)</p>
              <p class="text-2xl font-bold font-mono">{{ costStats?.avg_cost_per_upload || '0' }}</p>
            </div>
          </div>
          <div v-if="costStats?.by_department?.length">
            <h3 class="text-sm font-medium mb-2">By Department</h3>
            <div class="space-y-1">
              <div v-for="d in costStats.by_department" :key="d.department" class="flex justify-between text-sm">
                <span>{{ d.department || 'Unassigned' }}</span>
                <span class="text-surface-500 font-mono">{{ d.total_cost }}</span>
              </div>
            </div>
          </div>
        </template>
      </Card>
    </template>
  </div>
</template>
