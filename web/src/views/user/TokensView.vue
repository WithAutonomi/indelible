<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import { api } from '../../api/client'
import type { ApiToken } from '../../types/api'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import InputNumber from 'primevue/inputnumber'
import Select from 'primevue/select'
import Dialog from 'primevue/dialog'
import Card from 'primevue/card'
import Chip from 'primevue/chip'
import ConfirmDialog from 'primevue/confirmdialog'

const confirm = useConfirm()
const toast = useToast()

const tokens = ref<ApiToken[]>([])
const loading = ref(true)
const showCreate = ref(false)
const newTokenName = ref('')
const newTokenScope = ref('read')
const newTokenExpiry = ref(90)
const newTokenMaxSize = ref<number | null>(null)
const newTokenAllowedTypes = ref<string[]>([])
const newTokenAllowedTypesInput = ref('')
const createdTokenValue = ref('')
const creating = ref(false)

const scopeOptions = [
  { label: 'Read', value: 'read' },
  { label: 'Write', value: 'write' },
  { label: 'Admin', value: 'admin' },
]

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

function addAllowedType() {
  const v = newTokenAllowedTypesInput.value.trim()
  if (!v) return
  if (!newTokenAllowedTypes.value.includes(v)) {
    newTokenAllowedTypes.value.push(v)
  }
  newTokenAllowedTypesInput.value = ''
}

function removeAllowedType(idx: number) {
  newTokenAllowedTypes.value.splice(idx, 1)
}

async function createToken() {
  creating.value = true
  try {
    const payload: Record<string, unknown> = {
      name: newTokenName.value,
      permissions: [newTokenScope.value],
      expires_in_days: newTokenExpiry.value,
    }
    if (newTokenMaxSize.value && newTokenMaxSize.value > 0) {
      payload.max_file_size_bytes = newTokenMaxSize.value
    }
    if (newTokenAllowedTypes.value.length > 0) {
      payload.allowed_file_types = newTokenAllowedTypes.value
    }
    const res = await api.post('/api/v2/tokens', payload)
    createdTokenValue.value = res.data.secret
    newTokenName.value = ''
    newTokenMaxSize.value = null
    newTokenAllowedTypes.value = []
    newTokenAllowedTypesInput.value = ''
    toast.add({ severity: 'success', summary: 'Created', detail: 'API token created', life: 3000 })
    await fetchTokens()
  } catch (e: any) {
    toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to create token', life: 5000 })
  } finally {
    creating.value = false
  }
}

function revokeToken(id: number) {
  confirm.require({
    message: 'Revoke this token? This cannot be undone.',
    header: 'Confirm Revoke',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: async () => {
      try {
        await api.delete(`/api/v2/tokens/${id}`)
        toast.add({ severity: 'success', summary: 'Revoked', detail: 'Token revoked', life: 3000 })
        await fetchTokens()
      } catch {
        toast.add({ severity: 'error', summary: 'Error', detail: 'Failed to revoke token', life: 5000 })
      }
    },
  })
}

function copyToken() {
  navigator.clipboard.writeText(createdTokenValue.value)
}

function openCreate() {
  createdTokenValue.value = ''
  showCreate.value = true
}

function closeCreate() {
  showCreate.value = false
  createdTokenValue.value = ''
}

onMounted(fetchTokens)
</script>

<template>
  <div class="p-6">
    <ConfirmDialog />

    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold">API Tokens</h1>
      <Button label="New Token" icon="pi pi-plus" @click="openCreate" />
    </div>

    <!-- Create token dialog -->
    <Dialog v-model:visible="showCreate" header="Create API Token" modal :style="{ width: '30rem' }"
      :closable="!createdTokenValue">
      <!-- Show created token -->
      <div v-if="createdTokenValue" class="flex flex-col gap-4 pt-2">
        <p class="text-sm text-green-700">Token created! Copy it now -- you won't see it again.</p>
        <div class="flex gap-2">
          <code class="flex-1 bg-gray-100 px-3 py-2 rounded text-sm font-mono break-all">{{ createdTokenValue }}</code>
          <Button icon="pi pi-copy" severity="secondary" @click="copyToken" v-tooltip.top="'Copy'" />
        </div>
        <div class="flex justify-end">
          <Button label="Done" @click="closeCreate" />
        </div>
      </div>

      <!-- Create form -->
      <form v-else @submit.prevent="createToken" class="flex flex-col gap-4 pt-2">
        <div>
          <label class="block text-sm font-medium mb-1">Name</label>
          <InputText v-model="newTokenName" required placeholder="e.g. CI/CD Pipeline" class="w-full" />
        </div>
        <div>
          <label class="block text-sm font-medium mb-1">Scope</label>
          <Select v-model="newTokenScope" :options="scopeOptions" optionLabel="label" optionValue="value"
            class="w-48" />
        </div>
        <div>
          <label class="block text-sm font-medium mb-1">Expires In</label>
          <div class="flex items-center gap-2">
            <InputNumber v-model="newTokenExpiry" :min="1" :max="365" class="w-32" />
            <span class="text-sm text-gray-400">days</span>
          </div>
        </div>
        <div>
          <label class="block text-sm font-medium mb-1">Max upload size (optional)</label>
          <p class="text-xs text-surface-400 mb-2">Bytes. Tighter than your account limit; empty inherits the account/system limit.</p>
          <InputNumber v-model="newTokenMaxSize" :min="0" placeholder="Inherit account default" class="w-full" />
        </div>
        <div>
          <label class="block text-sm font-medium mb-1">Allowed file types (optional)</label>
          <p class="text-xs text-surface-400 mb-2">Content-type patterns (e.g. <code>image/*</code>, <code>application/pdf</code>). Empty inherits the account/system list.</p>
          <div class="flex gap-2 mb-2">
            <InputText v-model="newTokenAllowedTypesInput" placeholder="image/* or application/pdf" class="flex-1" @keydown.enter.prevent="addAllowedType" />
            <Button icon="pi pi-plus" severity="secondary" @click="addAllowedType" />
          </div>
          <div class="flex flex-wrap gap-2">
            <Chip v-for="(t, idx) in newTokenAllowedTypes" :key="t" :label="t" removable @remove="removeAllowedType(idx)" />
          </div>
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <Button label="Cancel" severity="secondary" text @click="closeCreate" />
          <Button type="submit" :label="creating ? 'Creating...' : 'Create Token'" :loading="creating" />
        </div>
      </form>
    </Dialog>

    <!-- Token list -->
    <Card>
      <template #content>
        <DataTable :value="tokens" :loading="loading" stripedRows
          :pt="{ root: { class: '-mt-2' } }">
          <template #empty>No API tokens yet.</template>
          <Column field="name" header="Name" sortable />
          <Column field="token_prefix" header="Prefix" sortable>
            <template #body="{ data }">
              <code class="text-sm text-gray-500">{{ data.token_prefix }}...</code>
            </template>
          </Column>
          <Column field="permissions" header="Scope" sortable />
          <Column field="request_count" header="Requests" sortable>
            <template #body="{ data }">{{ data.request_count || 0 }}</template>
          </Column>
          <Column field="last_used_at" header="Last Used" sortable>
            <template #body="{ data }">
              <span class="text-gray-400">{{ data.last_used_at ? new Date(data.last_used_at).toLocaleDateString() : 'Never' }}</span>
            </template>
          </Column>
          <Column field="expires_at" header="Expires" sortable>
            <template #body="{ data }">
              <span class="text-gray-400">{{ new Date(data.expires_at).toLocaleDateString() }}</span>
            </template>
          </Column>
          <Column header="Actions">
            <template #body="{ data }">
              <Button label="Revoke" icon="pi pi-trash" text severity="danger" size="small"
                @click="revokeToken(data.id)" />
            </template>
          </Column>
        </DataTable>
      </template>
    </Card>
  </div>
</template>
