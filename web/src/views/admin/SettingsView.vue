<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '../../api/client'

const settings = ref<Record<string, string>>({})
const loading = ref(true)
const saving = ref(false)
const saveMsg = ref('')

// Webhook state
const webhooks = ref<any[]>([])
const newWebhookUrl = ref('')
const creatingWebhook = ref(false)

// OIDC state
const oidcProviders = ref<any[]>([])

async function fetchSettings() {
  loading.value = true
  try {
    const res = await api.get('/api/v2/admin/settings')
    settings.value = res.data.settings || {}
  } catch {
    // ignore
  } finally {
    loading.value = false
  }
}

async function saveSettings() {
  saving.value = true
  saveMsg.value = ''
  try {
    await api.patch('/api/v2/admin/settings', settings.value)
    saveMsg.value = 'Settings saved.'
    setTimeout(() => saveMsg.value = '', 3000)
  } catch (e: any) {
    alert(e.response?.data?.error || 'Failed to save settings')
  } finally {
    saving.value = false
  }
}

async function exportSettings() {
  try {
    const res = await api.get('/api/v2/admin/settings/export')
    const blob = new Blob([JSON.stringify(res.data, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'indelible-settings.json'
    a.click()
    URL.revokeObjectURL(url)
  } catch {
    alert('Export failed')
  }
}

async function importSettings(e: Event) {
  const target = e.target as HTMLInputElement
  const file = target.files?.[0]
  if (!file) return
  const text = await file.text()
  try {
    const data = JSON.parse(text)
    await api.post('/api/v2/admin/settings/import', data)
    await fetchSettings()
    saveMsg.value = 'Settings imported.'
    setTimeout(() => saveMsg.value = '', 3000)
  } catch {
    alert('Import failed')
  }
}

async function fetchWebhooks() {
  try {
    const res = await api.get('/api/v2/admin/webhooks')
    webhooks.value = res.data.webhooks || []
  } catch {
    // ignore
  }
}

async function createWebhook() {
  if (!newWebhookUrl.value) return
  creatingWebhook.value = true
  try {
    await api.post('/api/v2/admin/webhooks', { url: newWebhookUrl.value })
    newWebhookUrl.value = ''
    await fetchWebhooks()
  } catch (e: any) {
    alert(e.response?.data?.error || 'Failed to create webhook')
  } finally {
    creatingWebhook.value = false
  }
}

async function deleteWebhook(id: number) {
  try {
    await api.delete(`/api/v2/admin/webhooks/${id}`)
    await fetchWebhooks()
  } catch {
    alert('Failed to delete webhook')
  }
}

async function fetchOIDC() {
  try {
    const res = await api.get('/api/v2/admin/oidc/providers')
    oidcProviders.value = res.data.providers || []
  } catch {
    // ignore
  }
}

const editableKeys = [
  'environment_name',
  'timezone',
  'max_upload_size_bytes',
  'default_visibility',
  'retention_days',
  'maintenance_mode',
]

onMounted(async () => {
  await Promise.all([fetchSettings(), fetchWebhooks(), fetchOIDC()])
})
</script>

<template>
  <div class="p-6 max-w-4xl">
    <h1 class="text-2xl font-bold mb-6">System Settings</h1>

    <div v-if="saveMsg" class="mb-4 rounded bg-green-50 p-3 text-green-700 text-sm">{{ saveMsg }}</div>

    <!-- Settings form -->
    <div class="bg-white rounded-lg border border-gray-200 p-6 mb-6">
      <div class="flex items-center justify-between mb-4">
        <h2 class="text-lg font-semibold">Configuration</h2>
        <div class="flex gap-2">
          <button @click="exportSettings" class="rounded border px-3 py-1.5 text-sm text-gray-600 hover:bg-gray-50">
            <i class="pi pi-download mr-1"></i> Export
          </button>
          <label class="rounded border px-3 py-1.5 text-sm text-gray-600 hover:bg-gray-50 cursor-pointer">
            <i class="pi pi-upload mr-1"></i> Import
            <input type="file" accept=".json" @change="importSettings" class="hidden" />
          </label>
        </div>
      </div>

      <div v-if="loading" class="text-center text-gray-400">Loading...</div>
      <form v-else @submit.prevent="saveSettings" class="space-y-4">
        <div v-for="key in editableKeys" :key="key">
          <label class="block text-sm font-medium text-gray-700 mb-1">{{ key }}</label>
          <input v-model="settings[key]" type="text"
            class="block w-full rounded border border-gray-300 px-3 py-2 text-sm" />
        </div>
        <button type="submit" :disabled="saving"
          class="rounded bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700 disabled:opacity-50">
          {{ saving ? 'Saving...' : 'Save Settings' }}
        </button>
      </form>
    </div>

    <!-- Webhooks -->
    <div class="bg-white rounded-lg border border-gray-200 p-6 mb-6">
      <h2 class="text-lg font-semibold mb-4">Webhooks</h2>
      <div class="flex gap-2 mb-4">
        <input v-model="newWebhookUrl" type="url" placeholder="https://example.com/webhook"
          class="flex-1 rounded border border-gray-300 px-3 py-2 text-sm" />
        <button @click="createWebhook" :disabled="creatingWebhook"
          class="rounded bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700 disabled:opacity-50">
          Add
        </button>
      </div>
      <div v-if="webhooks.length === 0" class="text-sm text-gray-400">No webhooks configured.</div>
      <div v-else class="divide-y divide-gray-100">
        <div v-for="w in webhooks" :key="w.id" class="flex items-center justify-between py-2">
          <div>
            <p class="text-sm font-mono text-gray-700">{{ w.url }}</p>
            <p class="text-xs text-gray-400">{{ w.integration_type }} &middot; {{ w.is_enabled ? 'Enabled' : 'Disabled' }}</p>
          </div>
          <button @click="deleteWebhook(w.id)" class="text-red-500 hover:text-red-700">
            <i class="pi pi-trash text-sm"></i>
          </button>
        </div>
      </div>
    </div>

    <!-- OIDC Providers -->
    <div class="bg-white rounded-lg border border-gray-200 p-6">
      <h2 class="text-lg font-semibold mb-4">SSO / OIDC Providers</h2>
      <div v-if="oidcProviders.length === 0" class="text-sm text-gray-400">No OIDC providers configured.</div>
      <div v-else class="divide-y divide-gray-100">
        <div v-for="p in oidcProviders" :key="p.id" class="flex items-center justify-between py-3">
          <div>
            <p class="text-sm font-medium text-gray-800">{{ p.display_name }}</p>
            <p class="text-xs text-gray-400">{{ p.issuer_url }} &middot; {{ p.is_enabled ? 'Enabled' : 'Disabled' }}</p>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
