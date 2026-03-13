<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '../../api/client'

const tokens = ref<any[]>([])
const loading = ref(true)
const showCreate = ref(false)
const newTokenName = ref('')
const newTokenScope = ref('read')
const newTokenExpiry = ref('90')
const createdTokenValue = ref('')
const creating = ref(false)

async function fetchTokens() {
  loading.value = true
  try {
    const res = await api.get('/api/v2/tokens')
    tokens.value = res.data.tokens || []
  } catch {
    // ignore
  } finally {
    loading.value = false
  }
}

async function createToken() {
  creating.value = true
  try {
    const res = await api.post('/api/v2/tokens', {
      name: newTokenName.value,
      scope: newTokenScope.value,
      expires_in_days: parseInt(newTokenExpiry.value),
    })
    createdTokenValue.value = res.data.token
    newTokenName.value = ''
    await fetchTokens()
  } catch (e: any) {
    alert(e.response?.data?.error || 'Failed to create token')
  } finally {
    creating.value = false
  }
}

async function revokeToken(id: number) {
  if (!confirm('Revoke this token? This cannot be undone.')) return
  try {
    await api.delete(`/api/v2/tokens/${id}`)
    await fetchTokens()
  } catch {
    alert('Failed to revoke token')
  }
}

function copyToken() {
  navigator.clipboard.writeText(createdTokenValue.value)
}

onMounted(fetchTokens)
</script>

<template>
  <div class="p-6 max-w-4xl">
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold">API Tokens</h1>
      <button @click="showCreate = !showCreate; createdTokenValue = ''"
        class="rounded bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700">
        <i class="pi pi-plus mr-1"></i> New Token
      </button>
    </div>

    <!-- Create token form -->
    <div v-if="showCreate" class="bg-white rounded-lg border border-gray-200 p-6 mb-6">
      <h2 class="text-lg font-semibold mb-4">Create API Token</h2>

      <!-- Show created token -->
      <div v-if="createdTokenValue" class="mb-4">
        <p class="text-sm text-green-700 mb-2">Token created! Copy it now — you won't see it again.</p>
        <div class="flex gap-2">
          <code class="flex-1 bg-gray-100 px-3 py-2 rounded text-sm font-mono break-all">{{ createdTokenValue }}</code>
          <button @click="copyToken" class="rounded bg-gray-200 px-3 py-2 text-sm hover:bg-gray-300">
            <i class="pi pi-copy"></i>
          </button>
        </div>
      </div>

      <form v-else @submit.prevent="createToken" class="space-y-4">
        <div>
          <label class="block text-sm font-medium text-gray-700 mb-1">Name</label>
          <input v-model="newTokenName" type="text" required placeholder="e.g. CI/CD Pipeline"
            class="block w-full rounded border border-gray-300 px-3 py-2 text-sm" />
        </div>
        <div class="grid grid-cols-2 gap-4">
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">Scope</label>
            <select v-model="newTokenScope" class="block w-full rounded border border-gray-300 px-3 py-2 text-sm">
              <option value="read">Read</option>
              <option value="write">Write</option>
              <option value="admin">Admin</option>
            </select>
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">Expires in (days)</label>
            <input v-model="newTokenExpiry" type="number" min="1" max="365"
              class="block w-full rounded border border-gray-300 px-3 py-2 text-sm" />
          </div>
        </div>
        <button type="submit" :disabled="creating"
          class="rounded bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700 disabled:opacity-50">
          {{ creating ? 'Creating...' : 'Create Token' }}
        </button>
      </form>
    </div>

    <!-- Token list -->
    <div class="bg-white rounded-lg border border-gray-200">
      <div v-if="loading" class="p-6 text-center text-gray-400">Loading...</div>
      <div v-else-if="tokens.length === 0" class="p-6 text-center text-gray-400">No API tokens yet.</div>
      <table v-else class="w-full">
        <thead class="text-left text-xs text-gray-500 uppercase bg-gray-50">
          <tr>
            <th class="px-6 py-3">Name</th>
            <th class="px-6 py-3">Prefix</th>
            <th class="px-6 py-3">Scope</th>
            <th class="px-6 py-3">Requests</th>
            <th class="px-6 py-3">Last Used</th>
            <th class="px-6 py-3">Expires</th>
            <th class="px-6 py-3">Actions</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100">
          <tr v-for="t in tokens" :key="t.id">
            <td class="px-6 py-3 text-sm font-medium text-gray-800">{{ t.name }}</td>
            <td class="px-6 py-3 text-sm font-mono text-gray-500">{{ t.token_prefix }}...</td>
            <td class="px-6 py-3 text-sm text-gray-500">{{ t.scope }}</td>
            <td class="px-6 py-3 text-sm text-gray-500">{{ t.request_count || 0 }}</td>
            <td class="px-6 py-3 text-sm text-gray-400">
              {{ t.last_used_at ? new Date(t.last_used_at).toLocaleDateString() : 'Never' }}
            </td>
            <td class="px-6 py-3 text-sm text-gray-400">
              {{ new Date(t.expires_at).toLocaleDateString() }}
            </td>
            <td class="px-6 py-3">
              <button @click="revokeToken(t.id)" class="text-red-600 hover:text-red-800 text-sm">
                <i class="pi pi-trash mr-1"></i>Revoke
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
