<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '../../api/client'

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

onMounted(fetchWallets)
</script>

<template>
  <div class="p-6 max-w-4xl">
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold">Wallets</h1>
      <button @click="showCreate = !showCreate"
        class="rounded bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700">
        <i class="pi pi-plus mr-1"></i> Add Wallet
      </button>
    </div>

    <!-- Create form -->
    <div v-if="showCreate" class="bg-white rounded-lg border border-gray-200 p-6 mb-6">
      <form @submit.prevent="createWallet" class="space-y-4">
        <div>
          <label class="block text-sm font-medium text-gray-700 mb-1">Name</label>
          <input v-model="newName" type="text" required placeholder="e.g. Production Wallet"
            class="block w-full rounded border border-gray-300 px-3 py-2 text-sm" />
        </div>
        <div>
          <label class="block text-sm font-medium text-gray-700 mb-1">Address</label>
          <input v-model="newAddress" type="text" required
            class="block w-full rounded border border-gray-300 px-3 py-2 text-sm font-mono" />
        </div>
        <div>
          <label class="block text-sm font-medium text-gray-700 mb-1">Private Key</label>
          <input v-model="newPrivateKey" type="password" required
            class="block w-full rounded border border-gray-300 px-3 py-2 text-sm font-mono" />
          <p class="text-xs text-gray-400 mt-1">Encrypted at rest with AES-256-GCM.</p>
        </div>
        <button type="submit" :disabled="creating"
          class="rounded bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700 disabled:opacity-50">
          {{ creating ? 'Creating...' : 'Add Wallet' }}
        </button>
      </form>
    </div>

    <!-- Wallet list -->
    <div class="bg-white rounded-lg border border-gray-200">
      <div v-if="loading" class="p-6 text-center text-gray-400">Loading...</div>
      <div v-else-if="wallets.length === 0" class="p-6 text-center text-gray-400">No wallets configured.</div>
      <table v-else class="w-full">
        <thead class="text-left text-xs text-gray-500 uppercase bg-gray-50">
          <tr>
            <th class="px-6 py-3">Name</th>
            <th class="px-6 py-3">Address</th>
            <th class="px-6 py-3">Default</th>
            <th class="px-6 py-3">Created</th>
            <th class="px-6 py-3">Actions</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100">
          <tr v-for="w in wallets" :key="w.id">
            <td class="px-6 py-3 text-sm font-medium text-gray-800">{{ w.name }}</td>
            <td class="px-6 py-3 text-sm font-mono text-gray-500">{{ w.address?.substring(0, 16) }}...</td>
            <td class="px-6 py-3">
              <span v-if="w.is_default" class="text-xs font-medium px-2 py-1 rounded text-green-700 bg-green-50">Default</span>
            </td>
            <td class="px-6 py-3 text-sm text-gray-400">{{ new Date(w.created_at).toLocaleDateString() }}</td>
            <td class="px-6 py-3">
              <button v-if="!w.is_default" @click="setDefault(w.id)"
                class="text-blue-600 hover:text-blue-800 text-sm">Set Default</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
