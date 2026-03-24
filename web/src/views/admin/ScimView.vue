<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '../../api/client'

interface ScimToken {
  id: number
  name: string
  is_active: boolean
  created_by: number
  last_used_at: string | null
  created_at: string
  revoked_at: string | null
}

const tokens = ref<ScimToken[]>([])
const loading = ref(true)
const scimEnabled = ref(false)
const loadingSettings = ref(true)

// Create form
const showCreateForm = ref(false)
const newName = ref('')
const creating = ref(false)
const newSecret = ref<string | null>(null)

function formatDate(d: string | null): string {
  if (!d) return 'Never'
  return new Date(d).toLocaleString()
}

async function fetchSettings() {
  loadingSettings.value = true
  try {
    const res = await api.get('/api/v2/admin/settings')
    scimEnabled.value = res.data.settings?.scim_enabled === 'true'
  } catch {
    // ignore
  } finally {
    loadingSettings.value = false
  }
}

async function toggleScim() {
  try {
    await api.patch('/api/v2/admin/settings', {
      scim_enabled: scimEnabled.value ? 'false' : 'true',
    })
    scimEnabled.value = !scimEnabled.value
  } catch {
    alert('Failed to update SCIM setting')
  }
}

async function fetchTokens() {
  loading.value = true
  try {
    const res = await api.get('/api/v2/admin/scim/tokens')
    tokens.value = res.data.tokens || []
  } catch {
    // ignore
  } finally {
    loading.value = false
  }
}

async function createToken() {
  if (!newName.value) return
  creating.value = true
  newSecret.value = null
  try {
    const res = await api.post('/api/v2/admin/scim/tokens', {
      name: newName.value,
    })
    newSecret.value = res.data.secret
    newName.value = ''
    await fetchTokens()
  } catch (e: any) {
    alert(e.response?.data?.error || 'Failed to create token')
  } finally {
    creating.value = false
  }
}

async function revokeToken(id: number) {
  if (!confirm('Revoke this SCIM token? Identity providers using it will lose access.')) return
  try {
    await api.delete(`/api/v2/admin/scim/tokens/${id}`)
    await fetchTokens()
  } catch {
    alert('Failed to revoke token')
  }
}

function copySecret() {
  if (newSecret.value) {
    navigator.clipboard.writeText(newSecret.value)
  }
}

const scimBaseUrl = `${window.location.origin}/scim/v2`

onMounted(() => {
  fetchSettings()
  fetchTokens()
})
</script>

<template>
  <div class="p-6">
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold">SCIM Provisioning</h1>
    </div>

    <div class="grid grid-cols-1 lg:grid-cols-3 gap-6">
      <!-- Configuration Card -->
      <div class="bg-white rounded-lg border border-gray-200">
        <div class="px-6 py-4 border-b border-gray-200">
          <h2 class="text-base font-semibold text-gray-800">Configuration</h2>
        </div>
        <div class="p-6 space-y-5">
          <div>
            <label class="text-sm font-medium text-gray-700">SCIM Provisioning</label>
            <div class="mt-2 flex items-center gap-3">
              <button type="button" @click="toggleScim" :disabled="loadingSettings"
                class="relative inline-flex h-6 w-11 items-center rounded-full transition-colors"
                :class="scimEnabled ? 'bg-blue-600' : 'bg-gray-200'">
                <span class="inline-block h-4 w-4 rounded-full bg-white transition-transform"
                  :class="scimEnabled ? 'translate-x-6' : 'translate-x-1'" />
              </button>
              <span class="text-sm text-gray-500">{{ scimEnabled ? 'Enabled' : 'Disabled' }}</span>
            </div>
          </div>

          <div>
            <label class="text-sm font-medium text-gray-700">SCIM Base URL</label>
            <div class="mt-1">
              <code class="text-sm bg-gray-100 px-3 py-1.5 rounded block font-mono text-gray-700 break-all">{{ scimBaseUrl }}</code>
            </div>
          </div>

          <div>
            <label class="text-sm font-medium text-gray-700">Supported Resources</label>
            <div class="mt-2 flex gap-2">
              <span class="text-xs px-2 py-1 rounded bg-blue-50 text-blue-700">Users</span>
              <span class="text-xs px-2 py-1 rounded bg-blue-50 text-blue-700">Groups</span>
            </div>
            <p class="text-xs text-gray-400 mt-2">
              SCIM 2.0 allows identity providers (Okta, Azure AD, Google Workspace) to automatically provision and deprovision users and groups.
            </p>
          </div>
        </div>
      </div>

      <!-- Tokens Card -->
      <div class="lg:col-span-2 bg-white rounded-lg border border-gray-200">
        <div class="px-6 py-4 border-b border-gray-200 flex items-center justify-between">
          <h2 class="text-base font-semibold text-gray-800">SCIM Tokens</h2>
          <button @click="showCreateForm = !showCreateForm; newSecret = null"
            class="rounded bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700">
            {{ showCreateForm ? 'Cancel' : 'Generate Token' }}
          </button>
        </div>

        <!-- Secret display (shown after creation) -->
        <div v-if="newSecret" class="mx-6 mt-4 p-4 bg-amber-50 border border-amber-200 rounded-lg">
          <p class="text-sm font-medium text-amber-800 mb-2">Copy this token now — it won't be shown again.</p>
          <div class="flex items-center gap-2">
            <code class="flex-1 text-sm bg-white px-3 py-2 rounded border border-amber-200 font-mono break-all select-all">{{ newSecret }}</code>
            <button @click="copySecret"
              class="shrink-0 rounded bg-amber-600 px-3 py-2 text-sm text-white hover:bg-amber-700">
              <i class="pi pi-copy"></i>
            </button>
          </div>
        </div>

        <!-- Create form -->
        <div v-if="showCreateForm && !newSecret" class="p-6 border-b border-gray-100">
          <div class="flex items-end gap-4">
            <div class="flex-1">
              <label class="text-sm font-medium text-gray-700">Token Name</label>
              <input v-model="newName" type="text" placeholder="e.g. Okta Production"
                class="mt-1 block w-full rounded border border-gray-300 px-3 py-2 text-sm"
                @keydown.enter.prevent="createToken" />
            </div>
            <button @click="createToken" :disabled="creating || !newName"
              class="rounded bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700 disabled:opacity-50">
              {{ creating ? 'Generating...' : 'Generate' }}
            </button>
          </div>
        </div>

        <!-- Tokens table -->
        <div v-if="loading" class="p-6 text-center text-gray-400">Loading...</div>
        <div v-else-if="tokens.length === 0" class="p-6 text-center text-gray-400">
          No SCIM tokens. Click "Generate Token" to create one for your identity provider.
        </div>
        <table v-else class="w-full">
          <thead class="text-left text-xs text-gray-500 uppercase bg-gray-50">
            <tr>
              <th class="px-6 py-3">Name</th>
              <th class="px-6 py-3">Created</th>
              <th class="px-6 py-3">Last Used</th>
              <th class="px-6 py-3">Status</th>
              <th class="px-6 py-3 text-right">Actions</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100">
            <tr v-for="t in tokens" :key="t.id" class="hover:bg-gray-50">
              <td class="px-6 py-3 text-sm font-medium text-gray-700">{{ t.name }}</td>
              <td class="px-6 py-3 text-sm text-gray-500">{{ formatDate(t.created_at) }}</td>
              <td class="px-6 py-3 text-sm text-gray-500">{{ formatDate(t.last_used_at) }}</td>
              <td class="px-6 py-3">
                <span class="text-xs font-medium px-2 py-0.5 rounded"
                  :class="t.is_active && !t.revoked_at ? 'bg-green-50 text-green-700' : 'bg-red-50 text-red-600'">
                  {{ t.is_active && !t.revoked_at ? 'Active' : 'Revoked' }}
                </span>
              </td>
              <td class="px-6 py-3 text-right">
                <button v-if="t.is_active && !t.revoked_at" @click="revokeToken(t.id)"
                  class="text-gray-400 hover:text-red-500" title="Revoke">
                  <i class="pi pi-ban text-sm"></i>
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>
