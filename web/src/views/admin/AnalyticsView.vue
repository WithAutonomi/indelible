<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '../../api/client'

const days = ref(7)
const uploadStats = ref<any>(null)
const tokenStats = ref<any>(null)
const costStats = ref<any>(null)
const loading = ref(true)

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
        <label class="text-sm text-gray-500">Period:</label>
        <select v-model="days" @change="fetchAll" class="rounded border border-gray-300 px-3 py-1.5 text-sm">
          <option :value="7">Last 7 days</option>
          <option :value="30">Last 30 days</option>
          <option :value="90">Last 90 days</option>
        </select>
      </div>
    </div>

    <div v-if="loading" class="text-center text-gray-400 py-12">Loading analytics...</div>

    <template v-else>
      <!-- Upload stats -->
      <div class="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
        <div class="bg-white rounded-lg border border-gray-200 p-4">
          <p class="text-xs text-gray-500 uppercase">Completed</p>
          <p class="text-2xl font-bold text-green-600">{{ uploadStats?.status_counts?.completed || 0 }}</p>
        </div>
        <div class="bg-white rounded-lg border border-gray-200 p-4">
          <p class="text-xs text-gray-500 uppercase">Queued</p>
          <p class="text-2xl font-bold text-yellow-600">{{ uploadStats?.status_counts?.queued || 0 }}</p>
        </div>
        <div class="bg-white rounded-lg border border-gray-200 p-4">
          <p class="text-xs text-gray-500 uppercase">Failed</p>
          <p class="text-2xl font-bold text-red-600">{{ uploadStats?.status_counts?.failed || 0 }}</p>
        </div>
        <div class="bg-white rounded-lg border border-gray-200 p-4">
          <p class="text-xs text-gray-500 uppercase">Avg Size</p>
          <p class="text-2xl font-bold text-gray-800">{{ formatBytes(uploadStats?.avg_file_size || 0) }}</p>
        </div>
      </div>

      <!-- Token stats -->
      <div class="bg-white rounded-lg border border-gray-200 p-6 mb-6">
        <h2 class="text-lg font-semibold mb-4">API Token Usage</h2>
        <div class="grid grid-cols-2 gap-4 mb-4">
          <div>
            <p class="text-xs text-gray-500 uppercase">Total Requests</p>
            <p class="text-2xl font-bold">{{ tokenStats?.total_requests || 0 }}</p>
          </div>
          <div>
            <p class="text-xs text-gray-500 uppercase">Active Tokens</p>
            <p class="text-2xl font-bold">{{ tokenStats?.active_tokens || 0 }}</p>
          </div>
        </div>
        <div v-if="tokenStats?.top_tokens?.length">
          <h3 class="text-sm font-medium text-gray-700 mb-2">Top Tokens</h3>
          <div class="space-y-1">
            <div v-for="t in tokenStats.top_tokens" :key="t.name" class="flex justify-between text-sm">
              <span class="text-gray-700">{{ t.name }}</span>
              <span class="text-gray-500">{{ t.request_count }} requests</span>
            </div>
          </div>
        </div>
      </div>

      <!-- Cost stats -->
      <div class="bg-white rounded-lg border border-gray-200 p-6">
        <h2 class="text-lg font-semibold mb-4">Storage Costs</h2>
        <div class="grid grid-cols-2 gap-4 mb-4">
          <div>
            <p class="text-xs text-gray-500 uppercase">Total Transactions</p>
            <p class="text-2xl font-bold">{{ costStats?.total_transactions || 0 }}</p>
          </div>
          <div>
            <p class="text-xs text-gray-500 uppercase">Total Spent (atto)</p>
            <p class="text-2xl font-bold font-mono">{{ costStats?.total_amount || '0' }}</p>
          </div>
        </div>
        <div v-if="costStats?.by_department?.length">
          <h3 class="text-sm font-medium text-gray-700 mb-2">By Department</h3>
          <div class="space-y-1">
            <div v-for="d in costStats.by_department" :key="d.department" class="flex justify-between text-sm">
              <span class="text-gray-700">{{ d.department || 'Unassigned' }}</span>
              <span class="text-gray-500 font-mono">{{ d.total_amount }}</span>
            </div>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>
