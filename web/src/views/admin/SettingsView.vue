<script setup lang="ts">
import { ref, reactive, onMounted, watch } from 'vue'
import { useToast } from 'primevue/usetoast'
import { api } from '../../api/client'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import InputNumber from 'primevue/inputnumber'
import Select from 'primevue/select'
import ToggleSwitch from 'primevue/toggleswitch'
import Message from 'primevue/message'
import Tabs from 'primevue/tabs'
import TabList from 'primevue/tablist'
import Tab from 'primevue/tab'
import TabPanels from 'primevue/tabpanels'
import TabPanel from 'primevue/tabpanel'
import Skeleton from 'primevue/skeleton'

const toast = useToast()
const loading = ref(true)

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

const visibilityOptions = [
  { label: 'Private', value: 'private' },
  { label: 'Public', value: 'public' },
]

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

    toast.add({ severity: 'success', summary: 'Saved', detail: 'Settings saved', life: 3000 })
  } catch (e: any) {
    toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to save settings', life: 5000 })
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
    toast.add({ severity: 'error', summary: 'Error', detail: 'Export failed', life: 5000 })
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
    toast.add({ severity: 'success', summary: 'Imported', detail: 'Settings imported', life: 3000 })
  } catch {
    toast.add({ severity: 'error', summary: 'Error', detail: 'Import failed', life: 5000 })
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
        <Button icon="pi pi-download" label="Export" severity="secondary" outlined @click="exportSettings" />
        <label>
          <Button icon="pi pi-upload" label="Import" severity="secondary" outlined as="span" class="cursor-pointer" />
          <input type="file" accept=".json" @change="importSettings" class="hidden" />
        </label>
      </div>
    </div>

    <div v-if="loading" class="space-y-4 py-4">
      <Skeleton height="2.5rem" width="16rem" />
      <Skeleton height="1px" />
      <div class="space-y-5">
        <div class="grid grid-cols-3 gap-6" v-for="i in 3" :key="i">
          <Skeleton height="1.5rem" width="10rem" />
          <Skeleton height="2.5rem" class="col-span-2" />
        </div>
      </div>
    </div>

    <Tabs v-else value="0">
      <TabList>
        <Tab value="0">General</Tab>
        <Tab value="1">Upload Limits</Tab>
        <Tab value="2">Operations</Tab>
        <Tab value="3">SSO / OIDC</Tab>
      </TabList>
      <TabPanels>
        <!-- General -->
        <TabPanel value="0">
          <div class="divide-y divide-surface-100">
            <div class="grid grid-cols-3 gap-6 py-5">
              <div>
                <label class="text-sm font-medium">Environment Name</label>
                <p class="text-xs text-surface-400 mt-1">Label shown in the UI header and exported settings.</p>
              </div>
              <div class="col-span-2">
                <InputText v-model="general.environment_name" class="w-full max-w-md" />
              </div>
            </div>
            <div class="grid grid-cols-3 gap-6 py-5">
              <div>
                <label class="text-sm font-medium">Timezone</label>
                <p class="text-xs text-surface-400 mt-1">For scheduled jobs, log timestamps, and date display.</p>
              </div>
              <div class="col-span-2">
                <InputText v-model="general.timezone" placeholder="e.g. Europe/London" class="w-full max-w-md" />
              </div>
            </div>
            <div class="grid grid-cols-3 gap-6 py-5">
              <div>
                <label class="text-sm font-medium">Default Visibility</label>
                <p class="text-xs text-surface-400 mt-1">Visibility for new uploads when not specified.</p>
              </div>
              <div class="col-span-2">
                <Select v-model="general.default_visibility" :options="visibilityOptions" optionLabel="label" optionValue="value" class="w-48" />
              </div>
            </div>
          </div>
          <div v-if="generalDirty" class="py-4 border-t border-surface-200 flex items-center justify-between">
            <p class="text-xs text-surface-500">You have unsaved changes</p>
            <div class="flex gap-2">
              <Button label="Discard" severity="secondary" outlined @click="discardCard('general')" />
              <Button :label="generalSaving ? 'Saving...' : 'Save'" :loading="generalSaving" @click="saveCard('general')" />
            </div>
          </div>
        </TabPanel>

        <!-- Upload Limits -->
        <TabPanel value="1">
          <div class="divide-y divide-surface-100">
            <div class="grid grid-cols-3 gap-6 py-5">
              <div>
                <label class="text-sm font-medium">Max Upload Size</label>
                <p class="text-xs text-surface-400 mt-1">Maximum file size per upload. 0 for unlimited.</p>
              </div>
              <div class="col-span-2">
                <div class="flex items-center gap-2">
                  <InputText v-model="uploads.max_upload_gb" type="number" class="w-32" />
                  <span class="text-sm text-surface-400">GB</span>
                </div>
              </div>
            </div>
            <div class="grid grid-cols-3 gap-6 py-5">
              <div>
                <label class="text-sm font-medium">Max Concurrent Uploads</label>
                <p class="text-xs text-surface-400 mt-1">Simultaneous uploads to the network. Higher values use more bandwidth.</p>
              </div>
              <div class="col-span-2">
                <InputText v-model="uploads.max_concurrent_uploads" type="number" class="w-32" />
              </div>
            </div>
            <div class="grid grid-cols-3 gap-6 py-5">
              <div>
                <label class="text-sm font-medium">Max Gas Fee</label>
                <p class="text-xs text-surface-400 mt-1">Max gas per upload in nanotokens. Uploads back off if exceeded. 0 for no limit.</p>
              </div>
              <div class="col-span-2">
                <div class="flex items-center gap-2">
                  <InputText v-model="uploads.max_gas_fee" type="number" class="w-40" />
                  <span class="text-sm text-surface-400">nanos</span>
                </div>
              </div>
            </div>
          </div>
          <div v-if="uploadsDirty" class="py-4 border-t border-surface-200 flex items-center justify-between">
            <p class="text-xs text-surface-500">You have unsaved changes</p>
            <div class="flex gap-2">
              <Button label="Discard" severity="secondary" outlined @click="discardCard('uploads')" />
              <Button :label="uploadsSaving ? 'Saving...' : 'Save'" :loading="uploadsSaving" @click="saveCard('uploads')" />
            </div>
          </div>
        </TabPanel>

        <!-- Operations -->
        <TabPanel value="2">
          <div class="divide-y divide-surface-100">
            <div class="grid grid-cols-3 gap-6 py-5">
              <div>
                <label class="text-sm font-medium">Maintenance Mode</label>
                <p class="text-xs text-surface-400 mt-1">Returns 503 for all non-admin API requests.</p>
              </div>
              <div class="col-span-2 flex items-center gap-3">
                <ToggleSwitch :modelValue="ops.maintenance_mode === 'true'"
                  @update:modelValue="ops.maintenance_mode = $event ? 'true' : 'false'" />
                <span class="text-sm text-surface-500">{{ ops.maintenance_mode === 'true' ? 'Enabled' : 'Disabled' }}</span>
              </div>
            </div>
            <div class="grid grid-cols-3 gap-6 py-5">
              <div>
                <label class="text-sm font-medium">Log Retention</label>
                <p class="text-xs text-surface-400 mt-1">Days to keep audit and system logs. 0 to keep indefinitely.</p>
              </div>
              <div class="col-span-2">
                <div class="flex items-center gap-2">
                  <InputText v-model="ops.log_retention_days" type="number" class="w-32" />
                  <span class="text-sm text-surface-400">days</span>
                </div>
              </div>
            </div>
          </div>
          <div v-if="opsDirty" class="py-4 border-t border-surface-200 flex items-center justify-between">
            <p class="text-xs text-surface-500">You have unsaved changes</p>
            <div class="flex gap-2">
              <Button label="Discard" severity="secondary" outlined @click="discardCard('ops')" />
              <Button :label="opsSaving ? 'Saving...' : 'Save'" :loading="opsSaving" @click="saveCard('ops')" />
            </div>
          </div>
        </TabPanel>

        <!-- SSO / OIDC -->
        <TabPanel value="3">
          <div v-if="oidcProviders.length === 0" class="py-6 text-sm text-surface-400">No OIDC providers configured.</div>
          <div v-else class="divide-y divide-surface-100">
            <div v-for="p in oidcProviders" :key="p.id" class="flex items-center justify-between py-4">
              <div>
                <p class="text-sm font-medium">{{ p.display_name }}</p>
                <p class="text-xs text-surface-400">{{ p.issuer_url }} &middot; {{ p.is_enabled ? 'Enabled' : 'Disabled' }}</p>
              </div>
            </div>
          </div>
        </TabPanel>
      </TabPanels>
    </Tabs>
  </div>
</template>
