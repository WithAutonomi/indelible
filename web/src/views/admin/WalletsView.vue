<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import { api } from '../../api/client'
import type { Wallet } from '../../types/api'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Password from 'primevue/password'
import Tag from 'primevue/tag'
import Message from 'primevue/message'
import Dialog from 'primevue/dialog'
import Drawer from 'primevue/drawer'

const route = useRoute()
const confirm = useConfirm()
const toast = useToast()
const wallets = ref<Wallet[]>([])
const loading = ref(true)
const showCreate = ref(false)
const newName = ref('')
const newPrivateKey = ref('')
const creating = ref(false)

async function fetchWallets() {
  loading.value = true
  try {
    const res = await api.get('/api/v2/admin/wallets')
    wallets.value = res.data.wallets || []
  } catch {
    // ignore
  } finally {
    loading.value = false
  }
}

async function createWallet() {
  creating.value = true
  try {
    await api.post('/api/v2/admin/wallets', {
      name: newName.value,
      private_key: newPrivateKey.value,
    })
    newName.value = ''
    newPrivateKey.value = ''
    showCreate.value = false
    await fetchWallets()
    toast.add({ severity: 'success', summary: 'Created', detail: 'Wallet added', life: 3000 })
  } catch (e: any) {
    toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to create wallet', life: 5000 })
  } finally {
    creating.value = false
  }
}

async function setDefault(id: number) {
  try {
    await api.put(`/api/v2/admin/wallets/${id}/default`)
    toast.add({ severity: 'success', summary: 'Updated', detail: 'Default wallet changed', life: 3000 })
    await fetchWallets()
  } catch {
    toast.add({ severity: 'error', summary: 'Error', detail: 'Failed to set default wallet', life: 5000 })
  }
}

function deleteWallet(id: number, name: string) {
  confirm.require({
    message: `Delete wallet "${name}"? This cannot be undone.`,
    header: 'Confirm Delete',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: async () => {
      try {
        await api.delete(`/api/v2/admin/wallets/${id}`)
        toast.add({ severity: 'success', summary: 'Deleted', detail: 'Wallet deleted', life: 3000 })
        await fetchWallets()
      } catch (e: any) {
        toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to delete wallet', life: 5000 })
      }
    },
  })
}

const refreshingBalance = ref<number | null>(null)

// V2-321: transaction history drawer
const txDrawerVisible = ref(false)
const txDrawerWallet = ref<Wallet | null>(null)
const transactions = ref<any[]>([])
const txLoading = ref(false)

async function openTransactions(wallet: Wallet) {
  txDrawerWallet.value = wallet
  txDrawerVisible.value = true
  txLoading.value = true
  try {
    const res = await api.get(`/api/v2/admin/wallets/${wallet.id}/transactions`, { params: { limit: 100 } })
    transactions.value = res.data.transactions || []
  } catch {
    transactions.value = []
  } finally {
    txLoading.value = false
  }
}

function truncateHash(h: string | null | undefined): string {
  if (!h) return '-'
  if (h.length <= 14) return h
  return h.slice(0, 8) + '…' + h.slice(-6)
}

async function refreshBalance(id: number) {
  refreshingBalance.value = id
  try {
    const res = await api.post(`/api/v2/admin/wallets/${id}/balance`)
    // Update the wallet in-place
    const w = wallets.value.find((w) => w.id === id)
    if (w) {
      w.payment_balance = res.data.payment_balance
      w.gas_balance = res.data.gas_balance
    }
  } catch (e: any) {
    toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to refresh balance', life: 5000 })
  } finally {
    refreshingBalance.value = null
  }
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

onMounted(() => {
  fetchWallets()
  if (route.query.add === '1') {
    showCreate.value = true
  }
})
</script>

<template>
  <div class="p-6">
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold">Wallets</h1>
      <Button icon="pi pi-plus" label="Add Wallet" @click="showCreate = !showCreate" />
    </div>

    <!-- No wallet setup prompt -->
    <Message v-if="!loading && wallets.length === 0 && !showCreate" severity="warn" :closable="false" class="mb-6">
      <div>
        <p class="font-medium">No wallets configured</p>
        <p class="text-sm mb-3">Add a wallet to enable file uploads to the Autonomi network. The first wallet added will automatically become the default.</p>
        <Button label="Add Your First Wallet" severity="warn" @click="showCreate = true" />
      </div>
    </Message>

    <!-- Create dialog -->
    <Dialog v-model:visible="showCreate" header="Add Wallet" modal :style="{ width: '30rem' }">
      <div class="space-y-5">
        <div>
          <label class="text-sm font-medium block mb-1">Name</label>
          <p class="text-xs text-surface-400 mb-2">A label for this wallet.</p>
          <InputText v-model="newName" required placeholder="e.g. Production Wallet" class="w-full" />
        </div>
        <div>
          <label class="text-sm font-medium block mb-1">Private Key</label>
          <p class="text-xs text-surface-400 mb-2">Encrypted at rest with AES-256-GCM. The wallet address is derived automatically.</p>
          <Password v-model="newPrivateKey" required :feedback="false" toggleMask inputClass="w-full font-mono" class="w-full" />
        </div>
      </div>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="showCreate = false" />
        <Button :label="creating ? 'Creating...' : 'Add Wallet'" :loading="creating" @click="createWallet" />
      </template>
    </Dialog>

    <!-- Wallet list -->
    <DataTable :value="wallets" :loading="loading" stripedRows class="rounded-lg border border-surface-200"
      :pt="{ root: { class: 'bg-surface-0' } }">
      <template #empty>No wallets configured.</template>
      <Column field="name" header="Name" sortable>
        <template #body="{ data }">
          <span class="font-medium">{{ data.name }}</span>
        </template>
      </Column>
      <Column field="address" header="Address" sortable>
        <template #body="{ data }">
          <span class="font-mono text-surface-500">{{ data.address?.substring(0, 16) }}...</span>
        </template>
      </Column>
      <Column field="payment_balance" header="Balance" sortable>
        <template #body="{ data }">
          <div class="flex items-start gap-2">
            <div>
              <div class="text-sm">
                <span class="font-medium">{{ formatBalance(data.payment_balance) }}</span>
                <span class="text-surface-400 ml-1">ANT</span>
              </div>
              <div class="text-xs text-surface-400">
                {{ formatBalance(data.gas_balance) }} gas
              </div>
            </div>
            <Button icon="pi pi-refresh" text rounded size="small" severity="secondary"
              :loading="refreshingBalance === data.id" aria-label="Refresh from chain" @click="refreshBalance(data.id)"
              v-tooltip.top="'Refresh from chain'" />
          </div>
        </template>
      </Column>
      <Column field="is_default" header="Default" sortable>
        <template #body="{ data }">
          <Tag v-if="data.is_default" value="Default" severity="success" />
        </template>
      </Column>
      <Column field="created_at" header="Created" sortable>
        <template #body="{ data }">
          <span class="text-surface-400">{{ new Date(data.created_at).toLocaleDateString() }}</span>
        </template>
      </Column>
      <Column header="Actions">
        <template #body="{ data }">
          <div class="flex gap-1 items-center">
            <Button v-if="!data.is_default" label="Set Default" severity="info" text size="small" @click="setDefault(data.id)" />
            <Button v-else label="Default" text size="small" disabled class="opacity-40"
              v-tooltip.top="'This is the active wallet'" />
            <Button icon="pi pi-history" text rounded size="small" severity="secondary"
              aria-label="Transaction history" v-tooltip.top="'Transaction history'"
              @click="openTransactions(data)" />
            <Button icon="pi pi-trash" text rounded size="small"
              :severity="data.is_default ? 'secondary' : 'secondary'"
              :disabled="data.is_default"
              :class="{ 'opacity-30': data.is_default }"
              aria-label="Delete wallet"
              @click="!data.is_default && deleteWallet(data.id, data.name)"
              v-tooltip.top="data.is_default ? 'Set another wallet as default before deleting' : 'Delete'" />
          </div>
        </template>
      </Column>
    </DataTable>

    <!-- V2-321: Transaction history drawer -->
    <Drawer v-model:visible="txDrawerVisible" position="right" class="w-full max-w-3xl"
      :header="txDrawerWallet ? `Transactions — ${txDrawerWallet.name}` : 'Transactions'">
      <div v-if="txDrawerWallet" class="flex items-center justify-between gap-3 mb-4">
        <p class="text-xs font-mono text-surface-500 truncate">{{ txDrawerWallet.address }}</p>
        <router-link :to="`/admin/transactions?wallet=${txDrawerWallet.id}`"
          class="text-sm text-primary font-medium hover:underline whitespace-nowrap shrink-0">
          Open full history →
        </router-link>
      </div>
      <DataTable :value="transactions" :loading="txLoading" stripedRows size="small">
        <template #empty>No transactions recorded for this wallet yet.</template>
        <Column field="created_at" header="Time" sortable>
          <template #body="{ data }">
            <span class="text-xs text-surface-500 whitespace-nowrap">{{ new Date(data.created_at).toLocaleString() }}</span>
          </template>
        </Column>
        <Column field="tx_type" header="Type" sortable>
          <template #body="{ data }">
            <Tag :value="data.tx_type" :severity="data.tx_type === 'refund' ? 'success' : 'info'" />
          </template>
        </Column>
        <Column field="amount" header="Amount (ANT)" sortable>
          <template #body="{ data }">
            <span class="font-mono text-sm">{{ formatBalance(data.amount) }}</span>
          </template>
        </Column>
        <Column field="balance_after" header="Balance After (ANT)" sortable>
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
    </Drawer>
  </div>
</template>
