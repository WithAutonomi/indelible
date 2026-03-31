<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../../api/client'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Password from 'primevue/password'
import Tag from 'primevue/tag'
import Message from 'primevue/message'

const route = useRoute()
const wallets = ref<any[]>([])
const loading = ref(true)
const showCreate = ref(false)
const newName = ref('')
const newAddress = ref('')
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
      address: newAddress.value,
      private_key: newPrivateKey.value,
    })
    newName.value = ''
    newAddress.value = ''
    newPrivateKey.value = ''
    showCreate.value = false
    await fetchWallets()
  } catch (e: any) {
    alert(e.response?.data?.error || 'Failed to create wallet')
  } finally {
    creating.value = false
  }
}

async function setDefault(id: number) {
  try {
    await api.put(`/api/v2/admin/wallets/${id}/default`)
    await fetchWallets()
  } catch {
    alert('Failed to set default wallet')
  }
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

    <!-- Create form -->
    <div v-if="showCreate" class="bg-surface-0 rounded-lg border border-surface-200 mb-6">
      <div class="px-6 py-4 border-b border-surface-200">
        <h2 class="text-base font-semibold">Add Wallet</h2>
      </div>
      <form @submit.prevent="createWallet">
        <div class="divide-y divide-surface-100">
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium">Name</label>
              <p class="text-xs text-surface-400 mt-1">A label for this wallet.</p>
            </div>
            <div class="col-span-2">
              <InputText v-model="newName" required placeholder="e.g. Production Wallet" class="w-full max-w-md" />
            </div>
          </div>
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium">Address</label>
              <p class="text-xs text-surface-400 mt-1">The wallet's public address.</p>
            </div>
            <div class="col-span-2">
              <InputText v-model="newAddress" required class="w-full max-w-lg font-mono" />
            </div>
          </div>
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium">Private Key</label>
              <p class="text-xs text-surface-400 mt-1">Encrypted at rest with AES-256-GCM.</p>
            </div>
            <div class="col-span-2">
              <Password v-model="newPrivateKey" required :feedback="false" toggleMask inputClass="w-full max-w-lg font-mono" class="w-full max-w-lg" />
            </div>
          </div>
        </div>
        <div class="px-6 py-4 bg-surface-50 border-t border-surface-200 rounded-b-lg flex justify-end gap-2">
          <Button type="button" label="Cancel" severity="secondary" outlined @click="showCreate = false" />
          <Button type="submit" :label="creating ? 'Creating...' : 'Add Wallet'" :loading="creating" />
        </div>
      </form>
    </div>

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
          <Button v-if="!data.is_default" label="Set Default" severity="info" text size="small" @click="setDefault(data.id)" />
        </template>
      </Column>
    </DataTable>
  </div>
</template>
