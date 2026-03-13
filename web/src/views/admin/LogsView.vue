<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '../../api/client'

const activeTab = ref<'audit' | 'system' | 'user'>('audit')
const entries = ref<any[]>([])
const total = ref(0)
const loading = ref(true)
const limit = 50
const page = ref(1)

// Filters
const eventType = ref('')
const level = ref('')
const since = ref('')
const until = ref('')

async function fetchLogs() {
  loading.value = true
  try {
    const params: any = {
      limit,
      offset: (page.value - 1) * limit,
    }
    if (since.value) params.since = since.value
    if (until.value) params.until = until.value

    let endpoint = ''
    if (activeTab.value === 'audit') {
      endpoint = '/api/v2/admin/logs/audit'
      if (eventType.value) params.event_type = eventType.value
    } else if (activeTab.value === 'system') {
      endpoint = '/api/v2/admin/logs/system'
      if (level.value) params.level = level.value
    } else {
      endpoint = '/api/v2/admin/logs/user'
    }

    const res = await api.get(endpoint, { params })
    entries.value = res.data.entries || []
    total.value = res.data.total || 0
  } catch {
    entries.value = []
  } finally {
    loading.value = false
  }
}

function switchTab(tab: 'audit' | 'system' | 'user') {
  activeTab.value = tab
  page.value = 1
  fetchLogs()
}

function severityClass(sev: string) {
  switch (sev) {
    case 'error': return 'text-red-700 bg-red-50'
    case 'warn': return 'text-yellow-700 bg-yellow-50'
    default: return 'text-blue-700 bg-blue-50'
  }
}

onMounted(fetchLogs)
</script>

<template>
  <div class="p-6">
    <h1 class="text-2xl font-bold mb-6">Logs</h1>

    <!-- Tabs -->
    <div class="flex gap-1 mb-4">
      <button v-for="tab in (['audit', 'system', 'user'] as const)" :key="tab"
        @click="switchTab(tab)"
        class="px-4 py-2 rounded-t text-sm font-medium"
        :class="activeTab === tab ? 'bg-white border border-b-0 border-gray-200 text-blue-700' : 'bg-gray-100 text-gray-600 hover:bg-gray-200'">
        {{ tab.charAt(0).toUpperCase() + tab.slice(1) }}
      </button>
    </div>

    <!-- Filters -->
    <div class="bg-white rounded-lg border border-gray-200 p-4 mb-4 flex flex-wrap gap-3 items-end">
      <div v-if="activeTab === 'audit'">
        <label class="block text-xs text-gray-500 mb-1">Event Type</label>
        <input v-model="eventType" type="text" placeholder="e.g. login"
          class="rounded border border-gray-300 px-3 py-1.5 text-sm w-36" />
      </div>
      <div v-if="activeTab === 'system'">
        <label class="block text-xs text-gray-500 mb-1">Level</label>
        <select v-model="level" class="rounded border border-gray-300 px-3 py-1.5 text-sm">
          <option value="">All</option>
          <option value="info">Info</option>
          <option value="warn">Warn</option>
          <option value="error">Error</option>
        </select>
      </div>
      <div>
        <label class="block text-xs text-gray-500 mb-1">Since</label>
        <input v-model="since" type="date" class="rounded border border-gray-300 px-3 py-1.5 text-sm" />
      </div>
      <div>
        <label class="block text-xs text-gray-500 mb-1">Until</label>
        <input v-model="until" type="date" class="rounded border border-gray-300 px-3 py-1.5 text-sm" />
      </div>
      <button @click="page = 1; fetchLogs()"
        class="rounded bg-gray-100 px-4 py-1.5 text-sm text-gray-700 hover:bg-gray-200">
        <i class="pi pi-search mr-1"></i> Filter
      </button>
    </div>

    <!-- Table -->
    <div class="bg-white rounded-lg border border-gray-200">
      <div v-if="loading" class="p-6 text-center text-gray-400">Loading...</div>
      <div v-else-if="entries.length === 0" class="p-6 text-center text-gray-400">No log entries found.</div>
      <table v-else class="w-full">
        <thead class="text-left text-xs text-gray-500 uppercase bg-gray-50">
          <tr>
            <th class="px-4 py-3">Time</th>
            <th v-if="activeTab !== 'system'" class="px-4 py-3">Event</th>
            <th v-if="activeTab === 'system'" class="px-4 py-3">Level</th>
            <th v-if="activeTab === 'system'" class="px-4 py-3">Component</th>
            <th class="px-4 py-3">{{ activeTab === 'system' ? 'Message' : 'Detail' }}</th>
            <th v-if="activeTab !== 'system'" class="px-4 py-3">Severity</th>
            <th v-if="activeTab === 'audit'" class="px-4 py-3">IP</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100">
          <tr v-for="e in entries" :key="e.id">
            <td class="px-4 py-2 text-xs text-gray-400 whitespace-nowrap">{{ new Date(e.created_at).toLocaleString() }}</td>
            <td v-if="activeTab !== 'system'" class="px-4 py-2 text-sm text-gray-700">{{ e.event_type }}</td>
            <td v-if="activeTab === 'system'" class="px-4 py-2">
              <span class="text-xs font-medium px-2 py-0.5 rounded" :class="severityClass(e.level)">{{ e.level }}</span>
            </td>
            <td v-if="activeTab === 'system'" class="px-4 py-2 text-sm text-gray-500">{{ e.component }}</td>
            <td class="px-4 py-2 text-sm text-gray-600 max-w-md truncate">{{ activeTab === 'system' ? e.message : e.detail }}</td>
            <td v-if="activeTab !== 'system'" class="px-4 py-2">
              <span class="text-xs font-medium px-2 py-0.5 rounded" :class="severityClass(e.severity)">{{ e.severity }}</span>
            </td>
            <td v-if="activeTab === 'audit'" class="px-4 py-2 text-xs text-gray-400">{{ e.ip_address || '-' }}</td>
          </tr>
        </tbody>
      </table>

      <div v-if="total > limit" class="flex items-center justify-between px-4 py-3 border-t border-gray-100">
        <p class="text-sm text-gray-500">{{ total }} total</p>
        <div class="flex gap-2">
          <button @click="page--; fetchLogs()" :disabled="page <= 1"
            class="rounded border px-3 py-1 text-sm disabled:opacity-50">Prev</button>
          <button @click="page++; fetchLogs()" :disabled="page * limit >= total"
            class="rounded border px-3 py-1 text-sm disabled:opacity-50">Next</button>
        </div>
      </div>
    </div>
  </div>
</template>
