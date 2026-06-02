<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import { api } from '../../api/client'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Tag from 'primevue/tag'
import ToggleSwitch from 'primevue/toggleswitch'
import Dialog from 'primevue/dialog'
import Message from 'primevue/message'

interface ScimToken {
  id: number
  name: string
  is_active: boolean
  created_by: number
  last_used_at: string | null
  created_at: string
  revoked_at: string | null
}

const confirm = useConfirm()
const toast = useToast()

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
    toast.add({ severity: 'success', summary: 'Updated', detail: `SCIM ${scimEnabled.value ? 'enabled' : 'disabled'}`, life: 3000 })
  } catch {
    toast.add({ severity: 'error', summary: 'Error', detail: 'Failed to update SCIM setting', life: 5000 })
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
    toast.add({ severity: 'success', summary: 'Created', detail: 'SCIM token created', life: 3000 })
    await fetchTokens()
  } catch (e: any) {
    toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to create token', life: 5000 })
  } finally {
    creating.value = false
  }
}

function revokeToken(id: number) {
  confirm.require({
    message: 'Revoke this SCIM token? Identity providers using it will lose access.',
    header: 'Confirm Revoke',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: async () => {
      try {
        await api.delete(`/api/v2/admin/scim/tokens/${id}`)
        await fetchTokens()
        toast.add({ severity: 'success', summary: 'Revoked', detail: 'SCIM token revoked', life: 3000 })
      } catch {
        toast.add({ severity: 'error', summary: 'Error', detail: 'Failed to revoke token', life: 5000 })
      }
    },
  })
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
      <a href="https://github.com/WithAutonomi/indelible/blob/master/docs/guides/scim.md" target="_blank" rel="noopener"
        class="inline-flex items-center gap-1 text-sm text-primary hover:underline">
        <i class="pi pi-book"></i> Setup guide
      </a>
    </div>

    <div class="grid grid-cols-1 lg:grid-cols-3 gap-6">
      <!-- Configuration Card -->
      <div class="bg-surface-0 rounded-lg border border-surface-200">
        <div class="px-6 py-4 border-b border-surface-200">
          <h2 class="text-base font-semibold">Configuration</h2>
        </div>
        <div class="p-6 space-y-5">
          <div>
            <label class="text-sm font-medium block mb-2">SCIM Provisioning</label>
            <div class="flex items-center gap-3">
              <ToggleSwitch :modelValue="scimEnabled" @update:modelValue="toggleScim" :disabled="loadingSettings" />
              <span class="text-sm text-surface-500">{{ scimEnabled ? 'Enabled' : 'Disabled' }}</span>
            </div>
          </div>

          <div>
            <label class="text-sm font-medium block mb-1">SCIM Base URL</label>
            <code class="text-sm bg-surface-100 px-3 py-1.5 rounded block font-mono break-all">{{ scimBaseUrl }}</code>
          </div>

          <div>
            <label class="text-sm font-medium block mb-2">Supported Resources</label>
            <div class="flex gap-2">
              <Tag value="Users" severity="info" />
              <Tag value="Groups" severity="info" />
            </div>
            <Message severity="info" :closable="false" class="mt-3">
              SCIM 2.0 allows identity providers (Okta, Azure AD, Google Workspace) to automatically provision and deprovision users and groups.
            </Message>
          </div>
        </div>
      </div>

      <!-- Tokens Card -->
      <div class="lg:col-span-2 bg-surface-0 rounded-lg border border-surface-200">
        <div class="px-6 py-4 border-b border-surface-200 flex items-center justify-between">
          <h2 class="text-base font-semibold">SCIM Tokens</h2>
          <Button label="Generate Token" icon="pi pi-plus" @click="showCreateForm = true; newSecret = null" />
        </div>

        <!-- Generate token dialog -->
        <Dialog v-model:visible="showCreateForm" header="Generate SCIM Token" modal :style="{ width: '30rem' }">
          <div v-if="!newSecret" class="space-y-4">
            <div>
              <label class="text-sm font-medium block mb-1">Token Name</label>
              <InputText v-model="newName" placeholder="e.g. Okta Production" class="w-full"
                @keydown.enter.prevent="createToken" />
            </div>
          </div>
          <div v-else>
            <Message severity="warn" :closable="false">
              <div>
                <p class="font-medium mb-2">Copy this token now -- it won't be shown again.</p>
                <div class="flex items-center gap-2">
                  <code class="flex-1 text-sm bg-white px-3 py-2 rounded border border-amber-200 font-mono break-all select-all">{{ newSecret }}</code>
                  <Button icon="pi pi-copy" severity="warn" @click="copySecret" />
                </div>
              </div>
            </Message>
          </div>
          <template #footer>
            <Button v-if="newSecret" label="Done" @click="showCreateForm = false; newSecret = null" />
            <template v-else>
              <Button label="Cancel" severity="secondary" text @click="showCreateForm = false" />
              <Button :label="creating ? 'Generating...' : 'Generate'" :loading="creating" :disabled="!newName" @click="createToken" />
            </template>
          </template>
        </Dialog>

        <!-- Tokens table -->
        <DataTable :value="tokens" :loading="loading" stripedRows>
          <template #empty>No SCIM tokens. Click "Generate Token" to create one for your identity provider.</template>
          <Column field="name" header="Name" sortable>
            <template #body="{ data }">
              <span class="font-medium">{{ data.name }}</span>
            </template>
          </Column>
          <Column field="created_at" header="Created" sortable>
            <template #body="{ data }">
              <span class="text-surface-500">{{ formatDate(data.created_at) }}</span>
            </template>
          </Column>
          <Column field="last_used_at" header="Last Used" sortable>
            <template #body="{ data }">
              <span class="text-surface-500">{{ formatDate(data.last_used_at) }}</span>
            </template>
          </Column>
          <Column field="is_active" header="Status" sortable>
            <template #body="{ data }">
              <Tag :value="data.is_active && !data.revoked_at ? 'Active' : 'Revoked'"
                :severity="data.is_active && !data.revoked_at ? 'success' : 'danger'" />
            </template>
          </Column>
          <Column header="Actions" style="width: 5rem; text-align: right">
            <template #body="{ data }">
              <Button v-if="data.is_active && !data.revoked_at" icon="pi pi-ban" severity="danger"
                text rounded size="small" v-tooltip.top="'Revoke'" @click="revokeToken(data.id)" />
            </template>
          </Column>
        </DataTable>
      </div>
    </div>
  </div>
</template>
