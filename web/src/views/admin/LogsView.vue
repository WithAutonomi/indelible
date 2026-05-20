<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '../../api/client'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import Tabs from 'primevue/tabs'
import TabList from 'primevue/tablist'
import Tab from 'primevue/tab'
import TabPanels from 'primevue/tabpanels'
import TabPanel from 'primevue/tabpanel'
import DatePicker from 'primevue/datepicker'

const activeTab = ref('audit')
const entries = ref<any[]>([])
const total = ref(0)
const loading = ref(true)
const limit = 50
const page = ref(1)

// Filters
const eventType = ref('')
const level = ref('')
const severity = ref('') // V2-318: shared by audit + user tabs
const settingKey = ref('') // V2-316
const sinceDate = ref<Date | null>(null)
const untilDate = ref<Date | null>(null)

const levelOptions = [
  { label: 'All', value: '' },
  { label: 'Info', value: 'info' },
  { label: 'Warn', value: 'warn' },
  { label: 'Error', value: 'error' },
]

const severityOptions = levelOptions // audit_log severity uses the same vocab

function formatDateParam(d: Date | null): string {
  if (!d) return ''
  return d.toISOString().split('T')[0]
}

async function fetchLogs() {
  loading.value = true
  try {
    const params: any = {
      limit,
      offset: (page.value - 1) * limit,
    }
    const sinceStr = formatDateParam(sinceDate.value)
    const untilStr = formatDateParam(untilDate.value)
    if (sinceStr) params.since = sinceStr
    if (untilStr) params.until = untilStr

    let endpoint = ''
    if (activeTab.value === 'audit') {
      endpoint = '/api/v2/admin/logs/audit'
      if (eventType.value) params.event_type = eventType.value
      if (severity.value) params.severity = severity.value
    } else if (activeTab.value === 'system') {
      endpoint = '/api/v2/admin/logs/system'
      if (level.value) params.level = level.value
    } else if (activeTab.value === 'config') {
      endpoint = '/api/v2/admin/logs/config'
      if (settingKey.value) params.setting_key = settingKey.value
    } else {
      endpoint = '/api/v2/admin/logs/user'
      if (severity.value) params.severity = severity.value
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

function switchTab(tab: string | number) {
  activeTab.value = tab as string
  page.value = 1
  fetchLogs()
  fetchStats()
}

function severitySeverity(sev: string): string {
  switch (sev) {
    case 'error': return 'danger'
    case 'warn': return 'warn'
    default: return 'info'
  }
}

const exporting = ref(false)

// V2-318: download the current view as JSONL. Fetched via the api client so
// the Bearer header is attached (window.open wouldn't carry it), then turned
// into a blob URL and clicked. The server caps at 1M rows.
async function exportCurrent() {
  const params: any = {}
  const sinceStr = formatDateParam(sinceDate.value)
  const untilStr = formatDateParam(untilDate.value)
  if (sinceStr) params.since = sinceStr
  if (untilStr) params.until = untilStr
  if (activeTab.value === 'audit') {
    if (eventType.value) params.event_type = eventType.value
    if (severity.value) params.severity = severity.value
  } else if (activeTab.value === 'system') {
    if (level.value) params.level = level.value
  } else if (activeTab.value === 'config') {
    if (settingKey.value) params.setting_key = settingKey.value
  } else if (severity.value) {
    params.severity = severity.value
  }

  exporting.value = true
  try {
    const res = await api.get(`/api/v2/admin/logs/${activeTab.value}/export`, {
      params,
      responseType: 'blob',
    })
    const blob = new Blob([res.data], { type: 'application/x-ndjson' })
    const url = URL.createObjectURL(blob)
    const filename = `${activeTab.value}-${new Date().toISOString().slice(0, 10)}.jsonl`
    const a = document.createElement('a')
    a.href = url
    a.download = filename
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(url)
  } catch {
    // ignore — surface via toast once we wire one in
  } finally {
    exporting.value = false
  }
}

// V2-319: stats per log type. Header card on each tab. The User tab is a
// filtered view of the audit log and shares the audit stats.
interface LogStats {
  total_entries: number
  earliest?: string
  latest?: string
  disk_usage_bytes: number
  by_severity?: Record<string, number>
  by_event_type?: Record<string, number>
  by_level?: Record<string, number>
  by_component?: Record<string, number>
  by_setting_key?: Record<string, number>
  by_day: Array<{ date: string; count: number }>
}

const stats = ref<LogStats | null>(null)
const loadingStats = ref(false)

function statsEndpointFor(tab: string): string {
  if (tab === 'system') return '/api/v2/admin/logs/system/stats'
  if (tab === 'config') return '/api/v2/admin/logs/config/stats'
  return '/api/v2/admin/logs/audit/stats' // audit + user share the audit table
}

async function fetchStats() {
  loadingStats.value = true
  try {
    const res = await api.get(statsEndpointFor(activeTab.value))
    stats.value = res.data
  } catch {
    stats.value = null
  } finally {
    loadingStats.value = false
  }
}

function formatBytes(n: number): string {
  if (!n) return '—'
  const units = ['B', 'KB', 'MB', 'GB']
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++ }
  return v.toFixed(v >= 100 ? 0 : 1) + ' ' + units[i]
}

function formatDateShort(s?: string): string {
  if (!s) return '—'
  return new Date(s).toLocaleDateString()
}

// Top-3 entries from a {key: count} map, ordered descending.
function topEntries(m: Record<string, number> | undefined, n = 3): Array<[string, number]> {
  if (!m) return []
  return Object.entries(m).sort((a, b) => b[1] - a[1]).slice(0, n)
}

// Sparkline path for the by_day array. Returns an SVG polyline points string.
function sparklinePoints(days: Array<{ date: string; count: number }> | undefined, width = 180, height = 32): string {
  if (!days || days.length === 0) return ''
  const max = Math.max(...days.map(d => d.count), 1)
  const step = width / Math.max(days.length - 1, 1)
  return days.map((d, i) => {
    const x = i * step
    const y = height - (d.count / max) * (height - 2) - 1
    return `${x.toFixed(1)},${y.toFixed(1)}`
  }).join(' ')
}

// Breakdown label per tab.
const breakdownTitle = (tab: string) => tab === 'system' ? 'Top components' : tab === 'config' ? 'Top setting keys' : 'Top events'

function breakdownEntries(tab: string): Array<[string, number]> {
  if (!stats.value) return []
  if (tab === 'system') return topEntries(stats.value.by_component)
  if (tab === 'config') return topEntries(stats.value.by_setting_key)
  return topEntries(stats.value.by_event_type)
}

// Refresh both list + stats whenever the tab changes or first mount.
function refreshAll() {
  fetchLogs()
  fetchStats()
}

onMounted(refreshAll)
</script>

<template>
  <div class="p-6">
    <h1 class="text-2xl font-bold mb-6">Logs</h1>

    <Tabs :value="activeTab" @update:value="switchTab">
      <TabList>
        <Tab value="audit">Audit</Tab>
        <Tab value="system">System</Tab>
        <Tab value="user">User</Tab>
        <Tab value="config">Config</Tab>
      </TabList>
      <TabPanels>
        <TabPanel v-for="tab in ['audit', 'system', 'user', 'config']" :key="tab" :value="tab">
          <!-- Stats header (V2-319) -->
          <div class="grid grid-cols-1 md:grid-cols-4 gap-3 mt-3 mb-4 p-4 bg-surface-50 rounded-lg border border-surface-200">
            <div>
              <div class="text-xs uppercase text-surface-500 font-medium">Total entries</div>
              <div class="text-xl font-semibold text-surface-800 mt-0.5">
                {{ loadingStats ? '…' : (stats?.total_entries ?? 0).toLocaleString() }}
              </div>
              <div v-if="stats?.disk_usage_bytes" class="text-xs text-surface-400 mt-0.5">{{ formatBytes(stats.disk_usage_bytes) }} on disk</div>
            </div>
            <div>
              <div class="text-xs uppercase text-surface-500 font-medium">Date range</div>
              <div class="text-sm text-surface-700 mt-1">
                {{ formatDateShort(stats?.earliest) }} → {{ formatDateShort(stats?.latest) }}
              </div>
            </div>
            <div>
              <div class="text-xs uppercase text-surface-500 font-medium">{{ breakdownTitle(activeTab) }}</div>
              <div class="text-xs text-surface-600 mt-1 space-y-0.5">
                <div v-for="[k, c] in breakdownEntries(activeTab)" :key="k" class="flex justify-between gap-2">
                  <span class="truncate font-mono">{{ k }}</span>
                  <span class="text-surface-400">{{ c }}</span>
                </div>
                <div v-if="!breakdownEntries(activeTab).length" class="text-surface-400">—</div>
              </div>
            </div>
            <div>
              <div class="text-xs uppercase text-surface-500 font-medium">Last 30 days</div>
              <svg :viewBox="`0 0 180 32`" class="w-full h-8 mt-1" preserveAspectRatio="none">
                <polyline :points="sparklinePoints(stats?.by_day)" fill="none" stroke="currentColor"
                  stroke-width="1.5" class="text-primary" />
              </svg>
            </div>
          </div>

          <!-- Filters -->
          <div class="flex flex-wrap gap-3 items-end mb-4 mt-2">
            <div v-if="activeTab === 'audit'">
              <label class="block text-xs text-surface-500 mb-1">Event Type</label>
              <InputText v-model="eventType" placeholder="e.g. login" class="w-36" size="small" />
            </div>
            <div v-if="activeTab === 'audit' || activeTab === 'user'">
              <label class="block text-xs text-surface-500 mb-1">Severity</label>
              <Select v-model="severity" :options="severityOptions" optionLabel="label" optionValue="value" class="w-32" />
            </div>
            <div v-if="activeTab === 'system'">
              <label class="block text-xs text-surface-500 mb-1">Level</label>
              <Select v-model="level" :options="levelOptions" optionLabel="label" optionValue="value" class="w-32" />
            </div>
            <div v-if="activeTab === 'config'">
              <label class="block text-xs text-surface-500 mb-1">Setting Key</label>
              <InputText v-model="settingKey" placeholder="e.g. maintenance_mode" class="w-48" size="small" />
            </div>
            <div>
              <label class="block text-xs text-surface-500 mb-1">Since</label>
              <DatePicker v-model="sinceDate" dateFormat="yy-mm-dd" showIcon class="w-40" />
            </div>
            <div>
              <label class="block text-xs text-surface-500 mb-1">Until</label>
              <DatePicker v-model="untilDate" dateFormat="yy-mm-dd" showIcon class="w-40" />
            </div>
            <Button icon="pi pi-search" label="Filter" severity="secondary" @click="page = 1; fetchLogs()" />
            <Button icon="pi pi-download" label="Export JSONL" severity="secondary" outlined
              :loading="exporting" @click="exportCurrent" />
          </div>

          <!-- Table -->
          <DataTable :value="entries" :loading="loading" stripedRows class="rounded-lg border border-surface-200"
            :pt="{ root: { class: 'bg-surface-0' } }">
            <template #empty>No log entries found.</template>
            <Column field="created_at" header="Time" sortable>
              <template #body="{ data }">
                <span class="text-xs text-surface-400 whitespace-nowrap">{{ new Date(data.created_at).toLocaleString() }}</span>
              </template>
            </Column>
            <Column v-if="activeTab === 'audit' || activeTab === 'user'" field="event_type" header="Event" sortable>
              <template #body="{ data }">
                <span class="text-sm">{{ data.event_type }}</span>
              </template>
            </Column>
            <Column v-if="activeTab === 'system'" field="level" header="Level" sortable>
              <template #body="{ data }">
                <Tag :value="data.level" :severity="severitySeverity(data.level)" />
              </template>
            </Column>
            <Column v-if="activeTab === 'system'" field="component" header="Component" sortable>
              <template #body="{ data }">
                <span class="text-sm text-surface-500">{{ data.component }}</span>
              </template>
            </Column>
            <Column v-if="activeTab === 'config'" field="setting_key" header="Setting" sortable>
              <template #body="{ data }">
                <code class="text-xs text-surface-600">{{ data.setting_key }}</code>
              </template>
            </Column>
            <Column v-if="activeTab === 'config'" field="old_value" header="Old" sortable>
              <template #body="{ data }">
                <span class="text-xs text-surface-400 max-w-xs truncate block font-mono"
                  :title="data.old_value || ''">{{ data.old_value || '-' }}</span>
              </template>
            </Column>
            <Column v-if="activeTab === 'config'" field="new_value" header="New" sortable>
              <template #body="{ data }">
                <span class="text-xs text-surface-700 max-w-xs truncate block font-mono"
                  :title="data.new_value || ''">{{ data.new_value || '-' }}</span>
              </template>
            </Column>
            <Column v-if="activeTab === 'config'" field="changed_by" header="Changed by" sortable>
              <template #body="{ data }">
                <span class="text-xs text-surface-500">{{ data.changed_by ?? '-' }}</span>
              </template>
            </Column>
            <Column v-if="activeTab !== 'config'" :field="activeTab === 'system' ? 'message' : 'detail'" :header="activeTab === 'system' ? 'Message' : 'Detail'" sortable>
              <template #body="{ data }">
                <span class="text-sm text-surface-600 max-w-md truncate block">{{ activeTab === 'system' ? data.message : data.detail }}</span>
              </template>
            </Column>
            <Column v-if="activeTab === 'audit' || activeTab === 'user'" field="severity" header="Severity" sortable>
              <template #body="{ data }">
                <Tag :value="data.severity" :severity="severitySeverity(data.severity)" />
              </template>
            </Column>
            <Column v-if="activeTab === 'audit' || activeTab === 'config'" field="ip_address" header="IP" sortable>
              <template #body="{ data }">
                <span class="text-xs text-surface-400">{{ data.ip_address || '-' }}</span>
              </template>
            </Column>
          </DataTable>

          <!-- Pagination -->
          <div v-if="total > limit" class="flex items-center justify-between mt-4">
            <p class="text-sm text-surface-500">{{ total }} total</p>
            <div class="flex gap-2">
              <Button label="Prev" severity="secondary" outlined size="small" :disabled="page <= 1"
                @click="page--; fetchLogs()" />
              <Button label="Next" severity="secondary" outlined size="small" :disabled="page * limit >= total"
                @click="page++; fetchLogs()" />
            </div>
          </div>
        </TabPanel>
      </TabPanels>
    </Tabs>
  </div>
</template>
