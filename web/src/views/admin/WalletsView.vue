<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../../api/client'

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
      <button @click="showCreate = !showCreate"
        class="rounded bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700">
        <i class="pi pi-plus mr-1"></i> Add Wallet
      </button>
    </div>

    <!-- No wallet setup prompt -->
    <div v-if="!loading && wallets.length === 0 && !showCreate" class="mb-6 rounded-lg border border-amber-300 bg-amber-50 p-4">
      <p class="text-sm font-medium text-amber-800">No wallets configured</p>
      <p class="text-sm text-amber-700 mb-3">Add a wallet to enable file uploads to the Autonomi network. The first wallet added will automatically become the default.</p>
      <button @click="showCreate = true"
        class="rounded bg-amber-600 px-4 py-2 text-sm text-white hover:bg-amber-700">
        Add Your First Wallet
      </button>
    </div>

    <!-- Create form -->
    <div v-if="showCreate" class="bg-white rounded-lg border border-gray-200 mb-6">
      <div class="px-6 py-4 border-b border-gray-200">
        <h2 class="text-base font-semibold text-gray-800">Add Wallet</h2>
      </div>
      <form @submit.prevent="createWallet">
        <div class="divide-y divide-gray-100">
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium text-gray-700">Name</label>
              <p class="text-xs text-gray-400 mt-1">A label for this wallet.</p>
            </div>
            <div class="col-span-2">
              <input v-model="newName" type="text" required placeholder="e.g. Production Wallet"
                class="block w-full max-w-md rounded border border-gray-300 px-3 py-2 text-sm" />
            </div>
          </div>
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium text-gray-700">Address</label>
              <p class="text-xs text-gray-400 mt-1">The wallet's public address.</p>
            </div>
            <div class="col-span-2">
              <input v-model="newAddress" type="text" required
                class="block w-full max-w-lg rounded border border-gray-300 px-3 py-2 text-sm font-mono" />
            </div>
          </div>
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium text-gray-700">Private Key</label>
              <p class="text-xs text-gray-400 mt-1">Encrypted at rest with AES-256-GCM.</p>
            </div>
            <div class="col-span-2">
              <input v-model="newPrivateKey" type="password" required
                class="block w-full max-w-lg rounded border border-gray-300 px-3 py-2 text-sm font-mono" />
            </div>
          </div>
        </div>
        <div class="px-6 py-4 bg-gray-50 border-t border-gray-200 rounded-b-lg flex justify-end gap-2">
          <button type="button" @click="showCreate = false"
            class="rounded border px-3 py-1.5 text-sm text-gray-600 hover:bg-gray-100">Cancel</button>
          <button type="submit" :disabled="creating"
            class="rounded bg-blue-600 px-4 py-1.5 text-sm text-white hover:bg-blue-700 disabled:opacity-50">
            {{ creating ? 'Creating...' : 'Add Wallet' }}
          </button>
        </div>
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
