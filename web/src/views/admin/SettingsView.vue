<script setup lang="ts">
import { ref, reactive, onMounted, watch } from 'vue'
import { api } from '../../api/client'

const loading = ref(true)
const globalMsg = ref('')

// OIDC state
const oidcProviders = ref<any[]>([])

// --- Per-card dirty tracking ---
// We store the "saved" snapshot and the "draft" for each card.
// Dirty = draft differs from saved.

// General card
const generalSaved = ref({ environment_name: '', timezone: '', default_visibility: 'private' })
const general = reactive({ environment_name: '', timezone: '', default_visibility: 'private' })
const generalDirty = ref(false)
const generalSaving = ref(false)

watch(general, () => {
  generalDirty.value =
    general.environment_name !== generalSaved.value.environment_name ||
    general.timezone !== generalSaved.value.timezone ||
    general.default_visibility !== generalSaved.value.default_visibility
}, { deep: true })

// Upload Limits card
const uploadsSaved = ref({ max_upload_gb: '0', max_concurrent_uploads: '', max_gas_fee: '' })
const uploads = reactive({ max_upload_gb: '0', max_concurrent_uploads: '', max_gas_fee: '' })
const uploadsDirty = ref(false)
const uploadsSaving = ref(false)

watch(uploads, () => {
  uploadsDirty.value =
    uploads.max_upload_gb !== uploadsSaved.value.max_upload_gb ||
    uploads.max_concurrent_uploads !== uploadsSaved.value.max_concurrent_uploads ||
    uploads.max_gas_fee !== uploadsSaved.value.max_gas_fee
}, { deep: true })

// Operations card
const opsSaved = ref({ maintenance_mode: 'false', log_retention_days: '' })
const ops = reactive({ maintenance_mode: 'false', log_retention_days: '' })
const opsDirty = ref(false)
const opsSaving = ref(false)

watch(ops, () => {
  opsDirty.value =
    ops.maintenance_mode !== opsSaved.value.maintenance_mode ||
    ops.log_retention_days !== opsSaved.value.log_retention_days
}, { deep: true })

// --- Helpers ---

function bytesToGB(bytes: string): string {
  const n = parseFloat(bytes)
  if (!n || n <= 0) return '0'
  return (n / 1073741824).toString()
}

function gbToBytes(gb: string): string {
  const n = parseFloat(gb)
  if (!n || n <= 0) return '0'
  return Math.round(n * 1073741824).toString()
}

// --- Fetch & hydrate ---

async function fetchSettings() {
  loading.value = true
  try {
    const res = await api.get('/api/v2/admin/settings')
    const s = res.data.settings || {}

    // General
    general.environment_name = s.environment_name || ''
    general.timezone = s.timezone || ''
    general.default_visibility = s.default_visibility || 'private'
    generalSaved.value = { ...general }

    // Uploads
    uploads.max_upload_gb = bytesToGB(s.max_upload_size_bytes || '0')
    uploads.max_concurrent_uploads = s.max_concurrent_uploads || ''
    uploads.max_gas_fee = s.max_gas_fee || ''
    uploadsSaved.value = { ...uploads }

    // Operations
    ops.maintenance_mode = s.maintenance_mode || 'false'
    ops.log_retention_days = s.log_retention_days || ''
    opsSaved.value = { ...ops }
  } catch {
    // ignore
  } finally {
    loading.value = false
  }
}

// --- Per-card save ---

async function saveCard(card: string) {
  let patch: Record<string, string> = {}

  if (card === 'general') {
    generalSaving.value = true
    patch = {
      environment_name: general.environment_name,
      timezone: general.timezone,
      default_visibility: general.default_visibility,
    }
  } else if (card === 'uploads') {
    uploadsSaving.value = true
    patch = {
      max_upload_size_bytes: gbToBytes(uploads.max_upload_gb),
      max_concurrent_uploads: uploads.max_concurrent_uploads,
      max_gas_fee: uploads.max_gas_fee,
    }
  } else if (card === 'ops') {
    opsSaving.value = true
    patch = {
      maintenance_mode: ops.maintenance_mode,
      log_retention_days: ops.log_retention_days,
    }
  }

  try {
    await api.patch('/api/v2/admin/settings', patch)

    // Update saved snapshot
    if (card === 'general') {
      generalSaved.value = { ...general }
      generalDirty.value = false
    } else if (card === 'uploads') {
      uploadsSaved.value = { ...uploads }
      uploadsDirty.value = false
    } else if (card === 'ops') {
      opsSaved.value = { ...ops }
      opsDirty.value = false
    }

    globalMsg.value = 'Settings saved.'
    setTimeout(() => globalMsg.value = '', 3000)
  } catch (e: any) {
    alert(e.response?.data?.error || 'Failed to save settings')
  } finally {
    generalSaving.value = false
    uploadsSaving.value = false
    opsSaving.value = false
  }
}

// --- Per-card discard ---

function discardCard(card: string) {
  if (card === 'general') {
    Object.assign(general, generalSaved.value)
  } else if (card === 'uploads') {
    Object.assign(uploads, uploadsSaved.value)
  } else if (card === 'ops') {
    Object.assign(ops, opsSaved.value)
  }
}

// --- Export / Import ---

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
    globalMsg.value = 'Settings imported.'
    setTimeout(() => globalMsg.value = '', 3000)
  } catch {
    alert('Import failed')
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

onMounted(async () => {
  await Promise.all([fetchSettings(), fetchOIDC()])
})
</script>

<template>
  <div class="p-6">
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold">System Settings</h1>
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

    <div v-if="globalMsg" class="mb-4 rounded bg-green-50 p-3 text-green-700 text-sm">{{ globalMsg }}</div>
    <div v-if="loading" class="text-center text-gray-400 py-12">Loading...</div>

    <div v-else class="space-y-6">
      <!-- Card: General -->
      <div class="bg-white rounded-lg border border-gray-200">
        <div class="px-6 py-4 border-b border-gray-200">
          <h2 class="text-base font-semibold text-gray-800">General</h2>
        </div>
        <div class="divide-y divide-gray-100">
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium text-gray-700">Environment Name</label>
              <p class="text-xs text-gray-400 mt-1">Label shown in the UI header and exported settings.</p>
            </div>
            <div class="col-span-2">
              <input v-model="general.environment_name" type="text"
                class="block w-full max-w-md rounded border border-gray-300 px-3 py-2 text-sm" />
            </div>
          </div>
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium text-gray-700">Timezone</label>
              <p class="text-xs text-gray-400 mt-1">For scheduled jobs, log timestamps, and date display.</p>
            </div>
            <div class="col-span-2">
              <input v-model="general.timezone" type="text" placeholder="e.g. Europe/London"
                class="block w-full max-w-md rounded border border-gray-300 px-3 py-2 text-sm" />
            </div>
          </div>
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium text-gray-700">Default Visibility</label>
              <p class="text-xs text-gray-400 mt-1">Visibility for new uploads when not specified.</p>
            </div>
            <div class="col-span-2">
              <select v-model="general.default_visibility"
                class="block w-48 rounded border border-gray-300 px-3 py-2 text-sm">
                <option value="private">Private</option>
                <option value="public">Public</option>
              </select>
            </div>
          </div>
        </div>
        <div v-if="generalDirty" class="px-6 py-4 bg-gray-50 border-t border-gray-200 rounded-b-lg flex items-center justify-between">
          <p class="text-xs text-gray-500">You have unsaved changes</p>
          <div class="flex gap-2">
            <button type="button" @click="discardCard('general')"
              class="rounded border px-3 py-1.5 text-sm text-gray-600 hover:bg-gray-100">
              Discard
            </button>
            <button type="button" @click="saveCard('general')" :disabled="generalSaving"
              class="rounded bg-blue-600 px-4 py-1.5 text-sm text-white hover:bg-blue-700 disabled:opacity-50">
              {{ generalSaving ? 'Saving...' : 'Save' }}
            </button>
          </div>
        </div>
      </div>

      <!-- Card: Upload Limits -->
      <div class="bg-white rounded-lg border border-gray-200">
        <div class="px-6 py-4 border-b border-gray-200">
          <h2 class="text-base font-semibold text-gray-800">Upload Limits</h2>
        </div>
        <div class="divide-y divide-gray-100">
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium text-gray-700">Max Upload Size</label>
              <p class="text-xs text-gray-400 mt-1">Maximum file size per upload. 0 for unlimited.</p>
            </div>
            <div class="col-span-2">
              <div class="flex items-center gap-2">
                <input v-model="uploads.max_upload_gb" type="number" step="0.1" min="0"
                  class="block w-32 rounded border border-gray-300 px-3 py-2 text-sm" />
                <span class="text-sm text-gray-400">GB</span>
              </div>
            </div>
          </div>
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium text-gray-700">Max Concurrent Uploads</label>
              <p class="text-xs text-gray-400 mt-1">Simultaneous uploads to the network. Higher values use more bandwidth.</p>
            </div>
            <div class="col-span-2">
              <input v-model="uploads.max_concurrent_uploads" type="number" min="1"
                class="block w-32 rounded border border-gray-300 px-3 py-2 text-sm" />
            </div>
          </div>
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium text-gray-700">Max Gas Fee</label>
              <p class="text-xs text-gray-400 mt-1">Max gas per upload in nanotokens. Uploads back off if exceeded. 0 for no limit.</p>
            </div>
            <div class="col-span-2">
              <div class="flex items-center gap-2">
                <input v-model="uploads.max_gas_fee" type="number" step="1" min="0"
                  class="block w-40 rounded border border-gray-300 px-3 py-2 text-sm" />
                <span class="text-sm text-gray-400">nanos</span>
              </div>
            </div>
          </div>
        </div>
        <div v-if="uploadsDirty" class="px-6 py-4 bg-gray-50 border-t border-gray-200 rounded-b-lg flex items-center justify-between">
          <p class="text-xs text-gray-500">You have unsaved changes</p>
          <div class="flex gap-2">
            <button type="button" @click="discardCard('uploads')"
              class="rounded border px-3 py-1.5 text-sm text-gray-600 hover:bg-gray-100">
              Discard
            </button>
            <button type="button" @click="saveCard('uploads')" :disabled="uploadsSaving"
              class="rounded bg-blue-600 px-4 py-1.5 text-sm text-white hover:bg-blue-700 disabled:opacity-50">
              {{ uploadsSaving ? 'Saving...' : 'Save' }}
            </button>
          </div>
        </div>
      </div>

      <!-- Card: Operations -->
      <div class="bg-white rounded-lg border border-gray-200">
        <div class="px-6 py-4 border-b border-gray-200">
          <h2 class="text-base font-semibold text-gray-800">Operations</h2>
        </div>
        <div class="divide-y divide-gray-100">
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium text-gray-700">Maintenance Mode</label>
              <p class="text-xs text-gray-400 mt-1">Returns 503 for all non-admin API requests.</p>
            </div>
            <div class="col-span-2 flex items-center">
              <button type="button"
                @click="ops.maintenance_mode = ops.maintenance_mode === 'true' ? 'false' : 'true'"
                class="relative inline-flex h-6 w-11 items-center rounded-full transition-colors"
                :class="ops.maintenance_mode === 'true' ? 'bg-blue-600' : 'bg-gray-200'">
                <span class="inline-block h-4 w-4 rounded-full bg-white transition-transform"
                  :class="ops.maintenance_mode === 'true' ? 'translate-x-6' : 'translate-x-1'" />
              </button>
              <span class="ml-3 text-sm text-gray-500">{{ ops.maintenance_mode === 'true' ? 'Enabled' : 'Disabled' }}</span>
            </div>
          </div>
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium text-gray-700">Log Retention</label>
              <p class="text-xs text-gray-400 mt-1">Days to keep audit and system logs. 0 to keep indefinitely.</p>
            </div>
            <div class="col-span-2">
              <div class="flex items-center gap-2">
                <input v-model="ops.log_retention_days" type="number" min="0"
                  class="block w-32 rounded border border-gray-300 px-3 py-2 text-sm" />
                <span class="text-sm text-gray-400">days</span>
              </div>
            </div>
          </div>
        </div>
        <div v-if="opsDirty" class="px-6 py-4 bg-gray-50 border-t border-gray-200 rounded-b-lg flex items-center justify-between">
          <p class="text-xs text-gray-500">You have unsaved changes</p>
          <div class="flex gap-2">
            <button type="button" @click="discardCard('ops')"
              class="rounded border px-3 py-1.5 text-sm text-gray-600 hover:bg-gray-100">
              Discard
            </button>
            <button type="button" @click="saveCard('ops')" :disabled="opsSaving"
              class="rounded bg-blue-600 px-4 py-1.5 text-sm text-white hover:bg-blue-700 disabled:opacity-50">
              {{ opsSaving ? 'Saving...' : 'Save' }}
            </button>
          </div>
        </div>
      </div>

      <!-- Card: OIDC Providers -->
      <div class="bg-white rounded-lg border border-gray-200">
        <div class="px-6 py-4 border-b border-gray-200">
          <h2 class="text-base font-semibold text-gray-800">SSO / OIDC Providers</h2>
        </div>
        <div class="px-6 py-5">
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
    </div>
  </div>
</template>
