<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../../api/client'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Select from 'primevue/select'
import Button from 'primevue/button'
import Tag from 'primevue/tag'
import DatePicker from 'primevue/datepicker'
import Card from 'primevue/card'
import { presetRange, PRESET_OPTIONS, type DatePreset } from '../../composables/useDateRangePresets'

const route = useRoute()

type Tx = {
  id: number
  wallet_id: number
  wallet_name: string
  tx_type: string
  amount: string
  balance_after: string
  upload_id: number | null
  tx_hash: string | null
  created_at: string
}

const transactions = ref<Tx[]>([])
const loading = ref(true)
const total = ref(0)
const page = ref(1)
const limit = 20

// Filters
const wallets = ref<{ id: number; name: string }[]>([])
const walletFilter = ref<number | null>(null)
const typeFilter = ref<string | null>(null)
const datePreset = ref<DatePreset>('')
const sinceDate = ref<Date | null>(null)
const untilDate = ref<Date | null>(null)

const walletOptions = ref<{ label: string; value: number | null }[]>([{ label: 'All wallets', value: null }])

const typeOptions = [
  { label: 'All types', value: null },
  { label: 'Upload (payment)', value: 'upload' },
  { label: 'Refund', value: 'refund' },
]

async function fetchWallets() {
  try {
    const res = await api.get('/api/v2/admin/wallets')
    wallets.value = (res.data.wallets || []).map((w: any) => ({ id: w.id, name: w.name }))
    walletOptions.value = [
      { label: 'All wallets', value: null },
      ...wallets.value.map((w) => ({ label: w.name, value: w.id })),
    ]
  } catch {
    wallets.value = []
  }
}

// V2-409 date convention: an active preset sends a precise rolling window;
// manual pickers send the selected day's start/end. Backend accepts RFC3339.
function applyDateParams(params: any) {
  const range = presetRange(datePreset.value)
  if (range) {
    params.from = range.since.toISOString()
    params.to = range.until.toISOString()
    return
  }
  if (sinceDate.value) {
    const s = new Date(sinceDate.value)
    s.setHours(0, 0, 0, 0)
    params.from = s.toISOString()
  }
  if (untilDate.value) {
    const u = new Date(untilDate.value)
    u.setHours(23, 59, 59, 999)
    params.to = u.toISOString()
  }
}

async function fetchTransactions() {
  loading.value = true
  try {
    const params: any = { limit, offset: (page.value - 1) * limit }
    if (walletFilter.value != null) params.wallet_id = walletFilter.value
    if (typeFilter.value) params.type = typeFilter.value
    applyDateParams(params)
    const res = await api.get('/api/v2/admin/transactions', { params })
    transactions.value = res.data.transactions || []
    total.value = res.data.total || 0
  } catch {
    transactions.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

function onFilterChange() {
  page.value = 1
  fetchTransactions()
}

function onDatePresetChange() {
  if (datePreset.value) {
    sinceDate.value = null
    untilDate.value = null
  }
  onFilterChange()
}

function onManualDate() {
  datePreset.value = ''
  onFilterChange()
}

function clearFilters() {
  walletFilter.value = null
  typeFilter.value = null
  datePreset.value = ''
  sinceDate.value = null
  untilDate.value = null
  onFilterChange()
}

function onPage(event: any) {
  page.value = event.page + 1
  fetchTransactions()
}

function formatBalance(atto: string): string {
  if (!atto || atto === '0' || atto === '') return '0'
  const n = BigInt(atto)
  const whole = n / BigInt(10 ** 18)
  const frac = n % BigInt(10 ** 18)
  if (frac === BigInt(0)) return whole.toString()
  const fracStr = frac.toString().padStart(18, '0').replace(/0+$/, '')
  return `${whole}.${fracStr}`
}

function truncateHash(h: string | null): string {
  if (!h) return '-'
  if (h.length <= 14) return h
  return h.slice(0, 8) + '…' + h.slice(-6)
}

onMounted(async () => {
  await fetchWallets()
  // Deep-link from the wallet drawer: /admin/transactions?wallet=<id>
  const w = route.query.wallet
  if (w != null && w !== '') {
    const id = Number(w)
    if (!Number.isNaN(id)) walletFilter.value = id
  }
  fetchTransactions()
})
</script>

<template>
  <div class="p-6">
    <div class="mb-6">
      <h1 class="text-2xl font-bold">Transactions</h1>
      <p class="text-sm text-surface-400 mt-1">
        Payment and refund history across all wallets.
      </p>
    </div>

    <!-- Filters -->
    <Card class="mb-4">
      <template #content>
        <div class="flex flex-wrap gap-3 items-end">
          <div>
            <label class="block text-xs text-surface-500 mb-1">Wallet</label>
            <Select v-model="walletFilter" :options="walletOptions" optionLabel="label" optionValue="value"
              size="small" class="w-48" @change="onFilterChange" />
          </div>
          <div>
            <label class="block text-xs text-surface-500 mb-1">Type</label>
            <Select v-model="typeFilter" :options="typeOptions" optionLabel="label" optionValue="value"
              size="small" class="w-40" @change="onFilterChange" />
          </div>
          <div>
            <label class="block text-xs text-surface-500 mb-1">Range</label>
            <Select v-model="datePreset" :options="PRESET_OPTIONS" optionLabel="label" optionValue="value"
              size="small" class="w-36" @change="onDatePresetChange" />
          </div>
          <div>
            <label class="block text-xs text-surface-500 mb-1">Since</label>
            <DatePicker v-model="sinceDate" dateFormat="yy-mm-dd" showIcon size="small" class="w-40"
              :disabled="!!datePreset" @date-select="onManualDate" />
          </div>
          <div>
            <label class="block text-xs text-surface-500 mb-1">Until</label>
            <DatePicker v-model="untilDate" dateFormat="yy-mm-dd" showIcon size="small" class="w-40"
              :disabled="!!datePreset" @date-select="onManualDate" />
          </div>
          <Button label="Clear" text size="small" @click="clearFilters" />
        </div>
      </template>
    </Card>

    <!-- Table -->
    <Card>
      <template #content>
        <DataTable :value="transactions" :loading="loading" stripedRows size="small"
          paginator :rows="limit" :totalRecords="total" :lazy="true" @page="onPage"
          paginatorTemplate="FirstPageLink PrevPageLink PageLinks NextPageLink LastPageLink CurrentPageReport"
          currentPageReportTemplate="Showing {first} to {last} of {totalRecords}"
          dataKey="id" :pt="{ root: { class: '-mt-2' } }">
          <template #empty>No transactions found.</template>
          <Column field="created_at" header="Time">
            <template #body="{ data }">
              <span class="text-xs text-surface-500 whitespace-nowrap">{{ new Date(data.created_at).toLocaleString() }}</span>
            </template>
          </Column>
          <Column field="wallet_name" header="Wallet">
            <template #body="{ data }">
              <span class="text-sm">{{ data.wallet_name || `#${data.wallet_id}` }}</span>
            </template>
          </Column>
          <Column field="tx_type" header="Type">
            <template #body="{ data }">
              <Tag :value="data.tx_type" :severity="data.tx_type === 'refund' ? 'success' : 'info'" />
            </template>
          </Column>
          <Column field="amount" header="Amount (ANT)">
            <template #body="{ data }">
              <span class="font-mono text-sm">{{ formatBalance(data.amount) }}</span>
            </template>
          </Column>
          <Column field="balance_after" header="Balance After (ANT)">
            <template #body="{ data }">
              <span class="font-mono text-sm text-surface-500">{{ formatBalance(data.balance_after) }}</span>
            </template>
          </Column>
          <Column field="upload_id" header="Upload">
            <template #body="{ data }">
              <span v-if="data.upload_id" class="text-xs text-surface-400">#{{ data.upload_id }}</span>
              <span v-else class="text-xs text-surface-300">-</span>
            </template>
          </Column>
          <Column field="tx_hash" header="Tx Hash">
            <template #body="{ data }">
              <code v-if="data.tx_hash" class="text-xs text-surface-400" :title="data.tx_hash">{{ truncateHash(data.tx_hash) }}</code>
              <span v-else class="text-xs text-surface-300">-</span>
            </template>
          </Column>
        </DataTable>
      </template>
    </Card>
  </div>
</template>
