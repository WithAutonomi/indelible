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
const adminGroups = ref<{ id: number; name: string }[]>([])
const savingProviderID = ref<number | null>(null)
const savingExtraParamsID = ref<number | null>(null)

// Extra-authorize-params editor uses an array of {key, value} rows per provider
// for ergonomic add/remove; we convert back to the API's object shape on save.
function rowsFromExtraParams(obj: Record<string, string> | null | undefined): { key: string; value: string }[] {
  if (!obj) return []
  return Object.entries(obj).map(([key, value]) => ({ key, value }))
}
function rowsToExtraParams(rows: { key: string; value: string }[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const row of rows) {
    const k = row.key.trim()
    if (k) out[k] = row.value
  }
  return out
}

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

// Notifier card — drives password reset + email verification delivery.
const notifierSaved = ref({ notifier_method: 'auto' })
const notifier = reactive({ notifier_method: 'auto' })
const notifierDirty = ref(false)
const notifierSaving = ref(false)

const notifierOptions = [
  { label: 'Auto (SMTP if configured, else webhook, else off)', value: 'auto' },
  { label: 'SMTP', value: 'smtp' },
  { label: 'Webhook', value: 'webhook' },
  { label: 'Disabled (no delivery)', value: 'noop' },
]

watch(notifier, () => {
  notifierDirty.value = notifier.notifier_method !== notifierSaved.value.notifier_method
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

    // Notifier
    notifier.notifier_method = s.notifier_method || 'auto'
    notifierSaved.value = { ...notifier }
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
  } else if (card === 'notifier') {
    notifierSaving.value = true
    patch = { notifier_method: notifier.notifier_method }
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
    } else if (card === 'notifier') {
      notifierSaved.value = { ...notifier }
      notifierDirty.value = false
    }

    toast.add({ severity: 'success', summary: 'Saved', detail: 'Settings saved', life: 3000 })
  } catch (e: any) {
    toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to save settings', life: 5000 })
  } finally {
    generalSaving.value = false
    uploadsSaving.value = false
    opsSaving.value = false
    notifierSaving.value = false
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
  } else if (card === 'notifier') {
    Object.assign(notifier, notifierSaved.value)
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
    const providers = res.data.providers || []
    // Attach the editable rows view of extra_authorize_params for the inline editor.
    providers.forEach((p: any) => {
      p._extraParamsRows = rowsFromExtraParams(p.extra_authorize_params)
    })
    oidcProviders.value = providers
  } catch {
    // ignore
  }
}

async function saveExtraParams(p: any) {
  savingExtraParamsID.value = p.id
  try {
    await api.put(`/api/v2/admin/oidc/providers/${p.id}/extra-params`, {
      extra_authorize_params: rowsToExtraParams(p._extraParamsRows),
    })
    toast.add({ severity: 'success', summary: 'Saved', detail: `${p.display_name} params updated`, life: 3000 })
  } catch (e: any) {
    toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to save', life: 5000 })
    await fetchOIDC()
  } finally {
    savingExtraParamsID.value = null
  }
}

async function fetchGroups() {
  try {
    const res = await api.get('/api/v2/admin/groups')
    adminGroups.value = (res.data.groups || []).map((g: any) => ({ id: g.id, name: g.name }))
  } catch {
    // ignore — auto-provision dropdown will fall back to "None" only.
  }
}

async function saveProviderAutoProvision(p: any) {
  savingProviderID.value = p.id
  try {
    await api.put(`/api/v2/admin/oidc/providers/${p.id}/auto-provision`, {
      auto_provision: !!p.auto_provision,
      default_group_id: p.default_group_id || 0,
    })
    toast.add({ severity: 'success', summary: 'Saved', detail: `${p.display_name} provisioning updated`, life: 3000 })
  } catch (e: any) {
    toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to save', life: 5000 })
    await fetchOIDC() // re-sync to backend truth
  } finally {
    savingProviderID.value = null
  }
}

onMounted(async () => {
  await Promise.all([fetchSettings(), fetchOIDC(), fetchGroups()])
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

          <div class="mt-8 divide-y divide-surface-100">
            <div class="grid grid-cols-3 gap-6 py-5">
              <div>
                <label class="text-sm font-medium">Email Notifier</label>
                <p class="text-xs text-surface-400 mt-1">
                  How password resets and email verification are delivered.
                  <span class="block mt-1">SMTP requires <code>SMTP_HOST</code> + <code>SMTP_FROM</code>;
                  Webhook requires at least one enabled webhook subscribed to
                  <code>auth.password_reset_requested</code> or
                  <code>auth.email_verification_requested</code>.</span>
                </p>
              </div>
              <div class="col-span-2">
                <Select v-model="notifier.notifier_method"
                  :options="notifierOptions"
                  optionLabel="label"
                  optionValue="value"
                  class="w-full max-w-md" />
                <p v-if="notifier.notifier_method === 'noop'" class="text-xs text-orange-500 mt-2">
                  &#9888; With "Disabled", users will never receive password-reset or verification emails.
                </p>
              </div>
            </div>
          </div>
          <div v-if="notifierDirty" class="py-4 border-t border-surface-200 flex items-center justify-between">
            <p class="text-xs text-surface-500">You have unsaved changes</p>
            <div class="flex gap-2">
              <Button label="Discard" severity="secondary" outlined @click="discardCard('notifier')" />
              <Button :label="notifierSaving ? 'Saving...' : 'Save'" :loading="notifierSaving" @click="saveCard('notifier')" />
            </div>
          </div>
        </TabPanel>

        <!-- SSO / OIDC -->
        <TabPanel value="3">
          <div v-if="oidcProviders.length === 0" class="py-6 text-sm text-surface-400">No OIDC providers configured.</div>
          <div v-else class="divide-y divide-surface-100">
            <div v-for="p in oidcProviders" :key="p.id" class="py-5">
              <div class="flex items-center justify-between mb-4">
                <div>
                  <p class="text-sm font-medium">{{ p.display_name }}</p>
                  <p class="text-xs text-surface-400">{{ p.issuer_url }} &middot; {{ p.is_enabled ? 'Enabled' : 'Disabled' }}</p>
                </div>
              </div>

              <div class="grid grid-cols-3 gap-6 py-3 pl-4">
                <div>
                  <label class="text-sm font-medium">Auto-provision</label>
                  <p class="text-xs text-surface-400 mt-1">
                    When on, an unknown sub or email pair signs in by creating a new local user.
                    When off, the same login is rejected with <code>no_account</code>.
                    <span class="block mt-1 text-orange-500">
                      Email-collision is always blocked — never auto-link by email alone.
                    </span>
                  </p>
                </div>
                <div class="col-span-2 flex items-center gap-3">
                  <ToggleSwitch v-model="p.auto_provision" />
                  <span class="text-sm text-surface-500">{{ p.auto_provision ? 'Enabled' : 'Disabled' }}</span>
                </div>
              </div>

              <div class="grid grid-cols-3 gap-6 py-3 pl-4">
                <div>
                  <label class="text-sm font-medium">Default group</label>
                  <p class="text-xs text-surface-400 mt-1">Auto-provisioned users join this group. Leave at <em>None</em> for no group assignment.</p>
                </div>
                <div class="col-span-2">
                  <Select
                    v-model="p.default_group_id"
                    :options="[{ id: 0, name: 'None' }, ...adminGroups]"
                    optionLabel="name"
                    optionValue="id"
                    class="w-full max-w-md"
                  />
                </div>
              </div>

              <div class="flex justify-end mt-3">
                <Button
                  :label="savingProviderID === p.id ? 'Saving...' : 'Save provisioning'"
                  :loading="savingProviderID === p.id"
                  size="small"
                  @click="saveProviderAutoProvision(p)"
                />
              </div>

              <div class="grid grid-cols-3 gap-6 py-3 pl-4 border-t border-surface-100 mt-3">
                <div>
                  <label class="text-sm font-medium">Extra authorize params</label>
                  <p class="text-xs text-surface-400 mt-1">
                    Extra query parameters appended to the IdP authorize URL. Most providers need none.
                    <span class="block mt-1">
                      <strong>Google Workspace:</strong> set <code>hd</code> = <code>your-domain.com</code> to restrict signin to your tenant
                      (without this, an "External" OAuth client accepts any personal <code>@gmail.com</code> account).
                    </span>
                    <span class="block mt-1"><strong>Microsoft / AAD:</strong> e.g. <code>prompt</code> = <code>select_account</code>, <code>domain_hint</code> = <code>your-domain.com</code>.</span>
                  </p>
                </div>
                <div class="col-span-2 space-y-2">
                  <div v-for="(row, i) in p._extraParamsRows" :key="i" class="flex gap-2 items-center">
                    <InputText v-model="row.key" placeholder="key (e.g. hd)" class="flex-1 max-w-xs" />
                    <InputText v-model="row.value" placeholder="value (e.g. company.com)" class="flex-1 max-w-xs" />
                    <Button
                      icon="pi pi-times"
                      severity="secondary"
                      outlined
                      size="small"
                      aria-label="Remove param"
                      @click="p._extraParamsRows.splice(i, 1)"
                    />
                  </div>
                  <Button
                    label="Add param"
                    icon="pi pi-plus"
                    severity="secondary"
                    outlined
                    size="small"
                    @click="p._extraParamsRows.push({ key: '', value: '' })"
                  />
                </div>
              </div>

              <div class="flex justify-end mt-3">
                <Button
                  :label="savingExtraParamsID === p.id ? 'Saving...' : 'Save params'"
                  :loading="savingExtraParamsID === p.id"
                  size="small"
                  severity="secondary"
                  outlined
                  @click="saveExtraParams(p)"
                />
              </div>
            </div>
          </div>
        </TabPanel>
      </TabPanels>
    </Tabs>
  </div>
</template>
