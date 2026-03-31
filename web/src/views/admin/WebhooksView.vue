<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '../../api/client'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Tag from 'primevue/tag'
import ToggleSwitch from 'primevue/toggleswitch'
import RadioButton from 'primevue/radiobutton'
import Checkbox from 'primevue/checkbox'
import Drawer from 'primevue/drawer'
import Message from 'primevue/message'

interface Webhook {
  id: number
  url: string
  integration_type: string
  is_enabled: boolean
  events: string
  created_at: string
  updated_at: string
}

interface Delivery {
  id: number
  webhook_id: number
  event_type: string
  status_code: number | null
  success: boolean
  attempts: number
  error_message?: string
  created_at: string
}

const webhooks = ref<Webhook[]>([])
const loading = ref(true)

// Create form
const showCreateForm = ref(false)
const newUrl = ref('')
const newType = ref('generic')
const newEvents = ref<string[]>(['completed', 'failed'])
const creating = ref(false)

// Edit state - now uses Drawer
const editingId = ref<number | null>(null)
const editDrawerVisible = ref(false)
const editUrl = ref('')
const editType = ref('generic')
const editEvents = ref<string[]>([])
const editEnabled = ref(true)
const saving = ref(false)

// Delivery history - also uses Drawer
const historyDrawerVisible = ref(false)
const historyWebhookUrl = ref('')
const deliveries = ref<Delivery[]>([])
const loadingHistory = ref(false)

// Secret display (shown once after create or rotate)
const webhookSecret = ref<string | null>(null)
const secretWebhookId = ref<number | null>(null)

// Test state
const testingId = ref<number | null>(null)
const testResult = ref<{ success: boolean; status_code: number; error?: string } | null>(null)

const uploadEvents = ['queued', 'processing', 'completed', 'failed']
const systemEvents = ['disk_warning', 'disk_critical', 'disk_recovered']

function parseEvents(eventsJson: string): string[] {
  try { return JSON.parse(eventsJson) } catch { return [] }
}

function formatDate(d: string): string {
  return new Date(d).toLocaleString()
}

async function fetchWebhooks() {
  loading.value = true
  try {
    const res = await api.get('/api/v2/admin/webhooks')
    webhooks.value = res.data.webhooks || []
  } catch {
    // ignore
  } finally {
    loading.value = false
  }
}

async function createWebhook() {
  if (!newUrl.value) return
  creating.value = true
  try {
    const res = await api.post('/api/v2/admin/webhooks', {
      url: newUrl.value,
      integration_type: newType.value,
      events: JSON.stringify(newEvents.value),
    })
    webhookSecret.value = res.data.secret || null
    secretWebhookId.value = res.data.webhook?.id || null
    newUrl.value = ''
    newType.value = 'generic'
    newEvents.value = ['completed', 'failed']
    showCreateForm.value = false
    await fetchWebhooks()
  } catch (e: any) {
    alert(e.response?.data?.error || 'Failed to create webhook')
  } finally {
    creating.value = false
  }
}

function startEdit(w: Webhook) {
  editingId.value = w.id
  editUrl.value = w.url
  editType.value = w.integration_type
  editEvents.value = parseEvents(w.events)
  editEnabled.value = w.is_enabled
  editDrawerVisible.value = true
}

async function saveEdit() {
  if (!editingId.value) return
  saving.value = true
  try {
    await api.patch(`/api/v2/admin/webhooks/${editingId.value}`, {
      url: editUrl.value,
      integration_type: editType.value,
      events: JSON.stringify(editEvents.value),
      is_enabled: editEnabled.value,
    })
    editDrawerVisible.value = false
    editingId.value = null
    await fetchWebhooks()
  } catch (e: any) {
    alert(e.response?.data?.error || 'Failed to update webhook')
  } finally {
    saving.value = false
  }
}

async function toggleWebhook(w: Webhook) {
  try {
    await api.patch(`/api/v2/admin/webhooks/${w.id}`, {
      url: w.url,
      integration_type: w.integration_type,
      events: w.events,
      is_enabled: !w.is_enabled,
    })
    await fetchWebhooks()
  } catch {
    alert('Failed to update webhook')
  }
}

async function deleteWebhook(id: number) {
  if (!confirm('Delete this webhook?')) return
  try {
    await api.delete(`/api/v2/admin/webhooks/${id}`)
    if (editingId.value === id) {
      editDrawerVisible.value = false
      editingId.value = null
    }
    await fetchWebhooks()
  } catch {
    alert('Failed to delete webhook')
  }
}

async function testWebhook(id: number) {
  testingId.value = id
  testResult.value = null
  try {
    const res = await api.post(`/api/v2/admin/webhooks/${id}/test`)
    testResult.value = res.data
    if (historyDrawerVisible.value) await fetchHistory(id)
  } catch (e: any) {
    testResult.value = { success: false, status_code: 0, error: e.response?.data?.error || 'Request failed' }
  } finally {
    setTimeout(() => {
      if (testingId.value === id) {
        testingId.value = null
        testResult.value = null
      }
    }, 5000)
  }
}

async function fetchHistory(id: number) {
  const w = webhooks.value.find(wh => wh.id === id)
  historyWebhookUrl.value = w?.url || ''
  historyDrawerVisible.value = true
  loadingHistory.value = true
  try {
    const res = await api.get(`/api/v2/admin/webhooks/${id}/deliveries?limit=20`)
    deliveries.value = res.data.deliveries || []
  } catch {
    deliveries.value = []
  } finally {
    loadingHistory.value = false
  }
}

async function rotateSecret(id: number) {
  if (!confirm('Rotate this webhook\'s signing secret? The old secret will stop working immediately.')) return
  try {
    const res = await api.post(`/api/v2/admin/webhooks/${id}/rotate-secret`)
    webhookSecret.value = res.data.secret
    secretWebhookId.value = id
  } catch {
    alert('Failed to rotate secret')
  }
}

function copySecret() {
  if (webhookSecret.value) {
    navigator.clipboard.writeText(webhookSecret.value)
  }
}

function eventTagSeverity(evt: string): string {
  if (systemEvents.includes(evt)) return 'warn'
  if (evt === 'test_ping') return 'secondary'
  return 'info'
}

onMounted(fetchWebhooks)
</script>

<template>
  <div class="p-6">
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold">Webhooks</h1>
      <Button :label="showCreateForm ? 'Cancel' : 'Add Endpoint'" :severity="showCreateForm ? 'secondary' : undefined"
        @click="showCreateForm = !showCreateForm" />
    </div>

    <!-- Create form -->
    <div v-if="showCreateForm" class="bg-surface-0 rounded-lg border border-surface-200 mb-6">
      <div class="px-6 py-4 border-b border-surface-200">
        <h2 class="text-base font-semibold">New Webhook</h2>
      </div>
      <div class="p-6 space-y-5">
        <!-- URL -->
        <div class="grid grid-cols-3 gap-6">
          <div>
            <label class="text-sm font-medium">Endpoint URL</label>
            <p class="text-xs text-surface-400 mt-1">URL that receives event notifications via POST.</p>
          </div>
          <div class="col-span-2">
            <InputText v-model="newUrl" type="url" placeholder="https://example.com/webhook"
              class="w-full max-w-lg font-mono" @keydown.enter.prevent="createWebhook" />
          </div>
        </div>

        <!-- Integration type -->
        <div class="grid grid-cols-3 gap-6">
          <div>
            <label class="text-sm font-medium">Integration Type</label>
            <p class="text-xs text-surface-400 mt-1">Generic sends raw JSON. Slack formats as Block Kit messages.</p>
          </div>
          <div class="col-span-2 flex gap-4">
            <div class="flex items-center gap-2">
              <RadioButton v-model="newType" inputId="newTypeGeneric" value="generic" />
              <label for="newTypeGeneric" class="text-sm cursor-pointer">Generic JSON</label>
            </div>
            <div class="flex items-center gap-2">
              <RadioButton v-model="newType" inputId="newTypeSlack" value="slack" />
              <label for="newTypeSlack" class="text-sm cursor-pointer">Slack</label>
            </div>
          </div>
        </div>

        <!-- Events -->
        <div class="grid grid-cols-3 gap-6">
          <div>
            <label class="text-sm font-medium">Events</label>
            <p class="text-xs text-surface-400 mt-1">Select which events trigger this webhook.</p>
          </div>
          <div class="col-span-2">
            <p class="text-xs font-medium text-surface-500 uppercase mb-2">Upload Events</p>
            <div class="flex flex-wrap gap-3 mb-3">
              <div v-for="evt in uploadEvents" :key="evt" class="flex items-center gap-1.5">
                <Checkbox v-model="newEvents" :inputId="'new-' + evt" :value="evt" />
                <label :for="'new-' + evt" class="text-sm cursor-pointer">{{ evt }}</label>
              </div>
            </div>
            <p class="text-xs font-medium text-surface-500 uppercase mb-2">System Events</p>
            <div class="flex flex-wrap gap-3">
              <div v-for="evt in systemEvents" :key="evt" class="flex items-center gap-1.5">
                <Checkbox v-model="newEvents" :inputId="'new-sys-' + evt" :value="evt" />
                <label :for="'new-sys-' + evt" class="text-sm cursor-pointer">{{ evt }}</label>
              </div>
            </div>
          </div>
        </div>
      </div>
      <div class="px-6 py-4 bg-surface-50 border-t border-surface-200 rounded-b-lg flex justify-end">
        <Button label="Create Webhook" :loading="creating" :disabled="!newUrl" @click="createWebhook" />
      </div>
    </div>

    <!-- Webhook secret display -->
    <Message v-if="webhookSecret" severity="warn" :closable="false" class="mb-6">
      <div>
        <p class="font-medium mb-2">Webhook signing secret -- copy now, it won't be shown again.</p>
        <div class="flex items-center gap-2">
          <code class="flex-1 text-sm bg-white px-3 py-2 rounded border border-amber-200 font-mono break-all select-all">{{ webhookSecret }}</code>
          <Button icon="pi pi-copy" severity="warn" @click="copySecret" />
          <Button label="Dismiss" severity="secondary" outlined @click="webhookSecret = null; secretWebhookId = null" />
        </div>
      </div>
    </Message>

    <!-- Webhooks list -->
    <div class="bg-surface-0 rounded-lg border border-surface-200">
      <div class="px-6 py-4 border-b border-surface-200">
        <h2 class="text-base font-semibold">Configured Endpoints</h2>
      </div>

      <DataTable :value="webhooks" :loading="loading" stripedRows>
        <template #empty>No webhooks configured. Click "Add Endpoint" to create one.</template>
        <Column field="url" header="URL">
          <template #body="{ data }">
            <span class="font-mono text-sm max-w-xs truncate block">{{ data.url }}</span>
          </template>
        </Column>
        <Column field="integration_type" header="Type" style="width: 8rem">
          <template #body="{ data }">
            <Tag :value="data.integration_type" :severity="data.integration_type === 'slack' ? 'danger' : 'secondary'" />
          </template>
        </Column>
        <Column header="Events">
          <template #body="{ data }">
            <div class="flex flex-wrap gap-1">
              <Tag v-for="evt in parseEvents(data.events)" :key="evt" :value="evt" :severity="eventTagSeverity(evt)" />
            </div>
          </template>
        </Column>
        <Column header="Status" style="width: 6rem">
          <template #body="{ data }">
            <ToggleSwitch :modelValue="data.is_enabled" @update:modelValue="toggleWebhook(data)" />
          </template>
        </Column>
        <Column header="Actions" style="width: 14rem">
          <template #body="{ data }">
            <div class="flex items-center gap-1">
              <!-- Test result toast -->
              <span v-if="testingId === data.id && testResult" class="text-xs mr-1"
                :class="testResult.success ? 'text-green-600' : 'text-red-500'">
                {{ testResult.success ? `OK (${testResult.status_code})` : (testResult.error || 'Failed') }}
              </span>
              <Button icon="pi pi-refresh" severity="warn" text rounded size="small"
                v-tooltip.top="'Rotate signing secret'" @click="rotateSecret(data.id)" />
              <Button icon="pi pi-bolt" severity="info" text rounded size="small"
                :disabled="testingId === data.id && !testResult" v-tooltip.top="'Send test ping'"
                @click="testWebhook(data.id)" />
              <Button icon="pi pi-history" severity="secondary" text rounded size="small"
                v-tooltip.top="'Delivery history'" @click="fetchHistory(data.id)" />
              <Button icon="pi pi-pencil" severity="secondary" text rounded size="small"
                v-tooltip.top="'Edit'" @click="startEdit(data)" />
              <Button icon="pi pi-trash" severity="danger" text rounded size="small"
                v-tooltip.top="'Delete'" @click="deleteWebhook(data.id)" />
            </div>
          </template>
        </Column>
      </DataTable>
    </div>

    <!-- Edit Drawer -->
    <Drawer v-model:visible="editDrawerVisible" header="Edit Webhook" position="right" class="w-full max-w-lg">
      <div class="space-y-5">
        <div>
          <label class="text-sm font-medium block mb-1">Endpoint URL</label>
          <InputText v-model="editUrl" type="url" class="w-full font-mono" />
        </div>

        <div>
          <label class="text-sm font-medium block mb-2">Integration Type</label>
          <div class="flex gap-4">
            <div class="flex items-center gap-2">
              <RadioButton v-model="editType" inputId="editTypeGeneric" value="generic" />
              <label for="editTypeGeneric" class="text-sm cursor-pointer">Generic JSON</label>
            </div>
            <div class="flex items-center gap-2">
              <RadioButton v-model="editType" inputId="editTypeSlack" value="slack" />
              <label for="editTypeSlack" class="text-sm cursor-pointer">Slack</label>
            </div>
          </div>
        </div>

        <div>
          <label class="text-sm font-medium block mb-2">Events</label>
          <p class="text-xs font-medium text-surface-500 uppercase mb-2">Upload</p>
          <div class="flex flex-wrap gap-3 mb-3">
            <div v-for="evt in uploadEvents" :key="evt" class="flex items-center gap-1.5">
              <Checkbox v-model="editEvents" :inputId="'edit-' + evt" :value="evt" />
              <label :for="'edit-' + evt" class="text-sm cursor-pointer">{{ evt }}</label>
            </div>
          </div>
          <p class="text-xs font-medium text-surface-500 uppercase mb-2">System</p>
          <div class="flex flex-wrap gap-3">
            <div v-for="evt in systemEvents" :key="evt" class="flex items-center gap-1.5">
              <Checkbox v-model="editEvents" :inputId="'edit-sys-' + evt" :value="evt" />
              <label :for="'edit-sys-' + evt" class="text-sm cursor-pointer">{{ evt }}</label>
            </div>
          </div>
        </div>

        <div>
          <label class="text-sm font-medium block mb-2">Enabled</label>
          <div class="flex items-center gap-3">
            <ToggleSwitch v-model="editEnabled" />
            <span class="text-sm text-surface-500">{{ editEnabled ? 'Enabled' : 'Disabled' }}</span>
          </div>
        </div>

        <div class="flex gap-2 pt-2">
          <Button :label="saving ? 'Saving...' : 'Save Changes'" :loading="saving" @click="saveEdit" />
          <Button label="Cancel" severity="secondary" outlined @click="editDrawerVisible = false" />
        </div>
      </div>
    </Drawer>

    <!-- Delivery History Drawer -->
    <Drawer v-model:visible="historyDrawerVisible" :header="'Delivery History'" position="right" class="w-full max-w-2xl">
      <p v-if="historyWebhookUrl" class="text-sm font-mono text-surface-500 mb-4 truncate">{{ historyWebhookUrl }}</p>
      <DataTable :value="deliveries" :loading="loadingHistory" stripedRows size="small">
        <template #empty>No delivery history.</template>
        <Column field="created_at" header="Time">
          <template #body="{ data }">
            <span class="text-surface-500 whitespace-nowrap">{{ formatDate(data.created_at) }}</span>
          </template>
        </Column>
        <Column field="event_type" header="Event">
          <template #body="{ data }">
            <Tag :value="data.event_type" :severity="eventTagSeverity(data.event_type)" />
          </template>
        </Column>
        <Column field="status_code" header="Status">
          <template #body="{ data }">
            <span class="text-surface-500">{{ data.status_code || '--' }}</span>
          </template>
        </Column>
        <Column field="attempts" header="Attempts">
          <template #body="{ data }">
            <span class="text-surface-500">{{ data.attempts }}</span>
          </template>
        </Column>
        <Column header="Result">
          <template #body="{ data }">
            <Tag :value="data.success ? 'OK' : (data.error_message || 'Failed')" :severity="data.success ? 'success' : 'danger'" />
          </template>
        </Column>
      </DataTable>
    </Drawer>
  </div>
</template>
