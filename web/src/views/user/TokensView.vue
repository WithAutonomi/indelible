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
      permissions: [newTokenScope.value],
      expires_in_days: parseInt(newTokenExpiry.value),
    })
    createdTokenValue.value = res.data.secret
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
  <div class="p-6">
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold">API Tokens</h1>
      <button @click="showCreate = !showCreate; createdTokenValue = ''"
        class="rounded bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700">
        <i class="pi pi-plus mr-1"></i> New Token
      </button>
    </div>

    <!-- Create token form -->
    <div v-if="showCreate" class="bg-white rounded-lg border border-gray-200 mb-6">
      <div class="px-6 py-4 border-b border-gray-200">
        <h2 class="text-base font-semibold text-gray-800">Create API Token</h2>
      </div>

      <!-- Show created token -->
      <div v-if="createdTokenValue" class="p-6">
        <p class="text-sm text-green-700 mb-2">Token created! Copy it now — you won't see it again.</p>
        <div class="flex gap-2 mb-3">
          <code class="flex-1 bg-gray-100 px-3 py-2 rounded text-sm font-mono break-all">{{ createdTokenValue }}</code>
          <button @click="copyToken" class="rounded bg-gray-200 px-3 py-2 text-sm hover:bg-gray-300">
            <i class="pi pi-copy"></i>
          </button>
        </div>
        <button @click="showCreate = false; createdTokenValue = ''"
          class="rounded border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50">
          Done
        </button>
      </div>

      <form v-else @submit.prevent="createToken">
        <div class="divide-y divide-gray-100">
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium text-gray-700">Name</label>
              <p class="text-xs text-gray-400 mt-1">A descriptive label for this token.</p>
            </div>
            <div class="col-span-2">
              <input v-model="newTokenName" type="text" required placeholder="e.g. CI/CD Pipeline"
                class="block w-full max-w-md rounded border border-gray-300 px-3 py-2 text-sm" />
            </div>
          </div>
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium text-gray-700">Scope</label>
              <p class="text-xs text-gray-400 mt-1">Permission level for API access.</p>
            </div>
            <div class="col-span-2">
              <select v-model="newTokenScope" class="block w-48 rounded border border-gray-300 px-3 py-2 text-sm">
                <option value="read">Read</option>
                <option value="write">Write</option>
                <option value="admin">Admin</option>
              </select>
            </div>
          </div>
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium text-gray-700">Expires In</label>
              <p class="text-xs text-gray-400 mt-1">Number of days until the token expires.</p>
            </div>
            <div class="col-span-2">
              <div class="flex items-center gap-2">
                <input v-model="newTokenExpiry" type="number" min="1" max="365"
                  class="block w-32 rounded border border-gray-300 px-3 py-2 text-sm" />
                <span class="text-sm text-gray-400">days</span>
              </div>
            </div>
          </div>
        </div>
        <div class="px-6 py-4 bg-gray-50 border-t border-gray-200 rounded-b-lg flex justify-end gap-2">
          <button type="button" @click="showCreate = false; createdTokenValue = ''"
            class="rounded border px-3 py-1.5 text-sm text-gray-600 hover:bg-gray-100">Cancel</button>
          <button type="submit" :disabled="creating"
            class="rounded bg-blue-600 px-4 py-1.5 text-sm text-white hover:bg-blue-700 disabled:opacity-50">
            {{ creating ? 'Creating...' : 'Create Token' }}
          </button>
        </div>
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
            <td class="px-6 py-3 text-sm text-gray-500">{{ t.permissions }}</td>
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
