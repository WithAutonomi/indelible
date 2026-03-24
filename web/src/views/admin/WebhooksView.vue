<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { api } from '../../api/client'

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

// Edit state
const editingId = ref<number | null>(null)
const editUrl = ref('')
const editType = ref('generic')
const editEvents = ref<string[]>([])
const editEnabled = ref(true)
const saving = ref(false)

// Delivery history
const historyId = ref<number | null>(null)
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
  if (editingId.value === w.id) {
    editingId.value = null
    return
  }
  editingId.value = w.id
  editUrl.value = w.url
  editType.value = w.integration_type
  editEvents.value = parseEvents(w.events)
  editEnabled.value = w.is_enabled
  historyId.value = null
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
    if (editingId.value === id) editingId.value = null
    if (historyId.value === id) historyId.value = null
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
    // Auto-refresh history if open
    if (historyId.value === id) await fetchHistory(id)
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
  if (historyId.value === id) {
    historyId.value = null
    return
  }
  historyId.value = id
  loadingHistory.value = true
  editingId.value = null
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

onMounted(fetchWebhooks)
</script>

<template>
  <div class="p-6">
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold">Webhooks</h1>
      <button @click="showCreateForm = !showCreateForm"
        class="rounded bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700">
        {{ showCreateForm ? 'Cancel' : 'Add Endpoint' }}
      </button>
    </div>

    <!-- Create form -->
    <div v-if="showCreateForm" class="bg-white rounded-lg border border-gray-200 mb-6">
      <div class="px-6 py-4 border-b border-gray-200">
        <h2 class="text-base font-semibold text-gray-800">New Webhook</h2>
      </div>
      <div class="p-6 space-y-5">
        <!-- URL -->
        <div class="grid grid-cols-3 gap-6">
          <div>
            <label class="text-sm font-medium text-gray-700">Endpoint URL</label>
            <p class="text-xs text-gray-400 mt-1">URL that receives event notifications via POST.</p>
          </div>
          <div class="col-span-2">
            <input v-model="newUrl" type="url" placeholder="https://example.com/webhook"
              class="block w-full max-w-lg rounded border border-gray-300 px-3 py-2 text-sm font-mono"
              @keydown.enter.prevent="createWebhook" />
          </div>
        </div>

        <!-- Integration type -->
        <div class="grid grid-cols-3 gap-6">
          <div>
            <label class="text-sm font-medium text-gray-700">Integration Type</label>
            <p class="text-xs text-gray-400 mt-1">Generic sends raw JSON. Slack formats as Block Kit messages.</p>
          </div>
          <div class="col-span-2 flex gap-4">
            <label class="flex items-center gap-2 cursor-pointer">
              <input type="radio" v-model="newType" value="generic" class="text-blue-600" />
              <span class="text-sm text-gray-700">Generic JSON</span>
            </label>
            <label class="flex items-center gap-2 cursor-pointer">
              <input type="radio" v-model="newType" value="slack" class="text-blue-600" />
              <span class="text-sm text-gray-700">Slack</span>
            </label>
          </div>
        </div>

        <!-- Events -->
        <div class="grid grid-cols-3 gap-6">
          <div>
            <label class="text-sm font-medium text-gray-700">Events</label>
            <p class="text-xs text-gray-400 mt-1">Select which events trigger this webhook.</p>
          </div>
          <div class="col-span-2">
            <p class="text-xs font-medium text-gray-500 uppercase mb-2">Upload Events</p>
            <div class="flex flex-wrap gap-3 mb-3">
              <label v-for="evt in uploadEvents" :key="evt" class="flex items-center gap-1.5 cursor-pointer">
                <input type="checkbox" :value="evt" v-model="newEvents" class="rounded text-blue-600" />
                <span class="text-sm text-gray-700">{{ evt }}</span>
              </label>
            </div>
            <p class="text-xs font-medium text-gray-500 uppercase mb-2">System Events</p>
            <div class="flex flex-wrap gap-3">
              <label v-for="evt in systemEvents" :key="evt" class="flex items-center gap-1.5 cursor-pointer">
                <input type="checkbox" :value="evt" v-model="newEvents" class="rounded text-blue-600" />
                <span class="text-sm text-gray-700">{{ evt }}</span>
              </label>
            </div>
          </div>
        </div>
      </div>
      <div class="px-6 py-4 bg-gray-50 border-t border-gray-200 rounded-b-lg flex justify-end">
        <button @click="createWebhook" :disabled="creating || !newUrl"
          class="rounded bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700 disabled:opacity-50">
          {{ creating ? 'Creating...' : 'Create Webhook' }}
        </button>
      </div>
    </div>

    <!-- Webhook secret display (shown once after create or rotate) -->
    <div v-if="webhookSecret" class="bg-amber-50 border border-amber-200 rounded-lg p-4 mb-6">
      <p class="text-sm font-medium text-amber-800 mb-2">Webhook signing secret — copy now, it won't be shown again.</p>
      <div class="flex items-center gap-2">
        <code class="flex-1 text-sm bg-white px-3 py-2 rounded border border-amber-200 font-mono break-all select-all">{{ webhookSecret }}</code>
        <button @click="copySecret"
          class="shrink-0 rounded bg-amber-600 px-3 py-2 text-sm text-white hover:bg-amber-700">
          <i class="pi pi-copy"></i>
        </button>
        <button @click="webhookSecret = null; secretWebhookId = null"
          class="shrink-0 rounded border border-gray-300 px-3 py-2 text-sm text-gray-600 hover:bg-gray-100">
          Dismiss
        </button>
      </div>
    </div>

    <!-- Webhooks list -->
    <div class="bg-white rounded-lg border border-gray-200">
      <div class="px-6 py-4 border-b border-gray-200">
        <h2 class="text-base font-semibold text-gray-800">Configured Endpoints</h2>
      </div>
      <div v-if="loading" class="p-6 text-center text-gray-400">Loading...</div>
      <div v-else-if="webhooks.length === 0" class="p-6 text-center text-gray-400">
        No webhooks configured. Click "Add Endpoint" to create one.
      </div>
      <div v-else>
        <table class="w-full">
          <thead class="text-left text-xs text-gray-500 uppercase bg-gray-50">
            <tr>
              <th class="px-6 py-3">URL</th>
              <th class="px-6 py-3">Type</th>
              <th class="px-6 py-3">Events</th>
              <th class="px-6 py-3">Status</th>
              <th class="px-6 py-3 text-right">Actions</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100">
            <template v-for="w in webhooks" :key="w.id">
              <!-- Main row -->
              <tr class="hover:bg-gray-50">
                <td class="px-6 py-3 text-sm font-mono text-gray-700 max-w-xs truncate">{{ w.url }}</td>
                <td class="px-6 py-3">
                  <span class="text-xs font-medium px-2 py-0.5 rounded"
                    :class="w.integration_type === 'slack' ? 'bg-purple-50 text-purple-700' : 'bg-gray-100 text-gray-600'">
                    {{ w.integration_type }}
                  </span>
                </td>
                <td class="px-6 py-3">
                  <div class="flex flex-wrap gap-1">
                    <span v-for="evt in parseEvents(w.events)" :key="evt"
                      class="text-xs px-1.5 py-0.5 rounded"
                      :class="systemEvents.includes(evt) ? 'bg-amber-50 text-amber-700' : 'bg-blue-50 text-blue-700'">
                      {{ evt }}
                    </span>
                  </div>
                </td>
                <td class="px-6 py-3">
                  <button @click.stop="toggleWebhook(w)"
                    class="relative inline-flex h-5 w-9 items-center rounded-full transition-colors"
                    :class="w.is_enabled ? 'bg-blue-600' : 'bg-gray-200'">
                    <span class="inline-block h-3.5 w-3.5 rounded-full bg-white transition-transform"
                      :class="w.is_enabled ? 'translate-x-4' : 'translate-x-0.5'" />
                  </button>
                </td>
                <td class="px-6 py-3 text-right">
                  <div class="flex items-center justify-end gap-2">
                    <!-- Test result toast -->
                    <span v-if="testingId === w.id && testResult" class="text-xs mr-1"
                      :class="testResult.success ? 'text-green-600' : 'text-red-500'">
                      {{ testResult.success ? `OK (${testResult.status_code})` : (testResult.error || 'Failed') }}
                    </span>
                    <button @click.stop="rotateSecret(w.id)"
                      class="text-gray-400 hover:text-amber-600" title="Rotate signing secret">
                      <i class="pi pi-refresh text-sm"></i>
                    </button>
                    <button @click.stop="testWebhook(w.id)" :disabled="testingId === w.id && !testResult"
                      class="text-gray-400 hover:text-blue-600 disabled:opacity-50" title="Send test ping">
                      <i class="pi pi-bolt text-sm"></i>
                    </button>
                    <button @click.stop="fetchHistory(w.id)"
                      class="text-gray-400 hover:text-gray-600" title="Delivery history"
                      :class="historyId === w.id ? 'text-blue-600' : ''">
                      <i class="pi pi-history text-sm"></i>
                    </button>
                    <button @click.stop="startEdit(w)"
                      class="text-gray-400 hover:text-gray-600" title="Edit"
                      :class="editingId === w.id ? 'text-blue-600' : ''">
                      <i class="pi pi-pencil text-sm"></i>
                    </button>
                    <button @click.stop="deleteWebhook(w.id)" class="text-gray-400 hover:text-red-500" title="Delete">
                      <i class="pi pi-trash text-sm"></i>
                    </button>
                  </div>
                </td>
              </tr>

              <!-- Edit panel -->
              <tr v-if="editingId === w.id">
                <td colspan="5" class="bg-gray-50 px-6 py-5 border-t border-gray-100">
                  <div class="space-y-4">
                    <div class="grid grid-cols-3 gap-6">
                      <div>
                        <label class="text-sm font-medium text-gray-700">Endpoint URL</label>
                      </div>
                      <div class="col-span-2">
                        <input v-model="editUrl" type="url"
                          class="block w-full max-w-lg rounded border border-gray-300 px-3 py-2 text-sm font-mono" />
                      </div>
                    </div>
                    <div class="grid grid-cols-3 gap-6">
                      <div>
                        <label class="text-sm font-medium text-gray-700">Integration Type</label>
                      </div>
                      <div class="col-span-2 flex gap-4">
                        <label class="flex items-center gap-2 cursor-pointer">
                          <input type="radio" v-model="editType" value="generic" class="text-blue-600" />
                          <span class="text-sm text-gray-700">Generic JSON</span>
                        </label>
                        <label class="flex items-center gap-2 cursor-pointer">
                          <input type="radio" v-model="editType" value="slack" class="text-blue-600" />
                          <span class="text-sm text-gray-700">Slack</span>
                        </label>
                      </div>
                    </div>
                    <div class="grid grid-cols-3 gap-6">
                      <div>
                        <label class="text-sm font-medium text-gray-700">Events</label>
                      </div>
                      <div class="col-span-2">
                        <p class="text-xs font-medium text-gray-500 uppercase mb-2">Upload</p>
                        <div class="flex flex-wrap gap-3 mb-3">
                          <label v-for="evt in uploadEvents" :key="evt" class="flex items-center gap-1.5 cursor-pointer">
                            <input type="checkbox" :value="evt" v-model="editEvents" class="rounded text-blue-600" />
                            <span class="text-sm text-gray-700">{{ evt }}</span>
                          </label>
                        </div>
                        <p class="text-xs font-medium text-gray-500 uppercase mb-2">System</p>
                        <div class="flex flex-wrap gap-3">
                          <label v-for="evt in systemEvents" :key="evt" class="flex items-center gap-1.5 cursor-pointer">
                            <input type="checkbox" :value="evt" v-model="editEvents" class="rounded text-blue-600" />
                            <span class="text-sm text-gray-700">{{ evt }}</span>
                          </label>
                        </div>
                      </div>
                    </div>
                    <div class="grid grid-cols-3 gap-6">
                      <div>
                        <label class="text-sm font-medium text-gray-700">Enabled</label>
                      </div>
                      <div class="col-span-2 flex items-center">
                        <button type="button" @click="editEnabled = !editEnabled"
                          class="relative inline-flex h-6 w-11 items-center rounded-full transition-colors"
                          :class="editEnabled ? 'bg-blue-600' : 'bg-gray-200'">
                          <span class="inline-block h-4 w-4 rounded-full bg-white transition-transform"
                            :class="editEnabled ? 'translate-x-6' : 'translate-x-1'" />
                        </button>
                        <span class="ml-3 text-sm text-gray-500">{{ editEnabled ? 'Enabled' : 'Disabled' }}</span>
                      </div>
                    </div>
                    <div class="flex gap-2 pt-2">
                      <button @click="saveEdit" :disabled="saving"
                        class="rounded bg-blue-600 px-4 py-1.5 text-sm text-white hover:bg-blue-700 disabled:opacity-50">
                        {{ saving ? 'Saving...' : 'Save Changes' }}
                      </button>
                      <button @click="editingId = null"
                        class="rounded border px-3 py-1.5 text-sm text-gray-600 hover:bg-gray-100">
                        Cancel
                      </button>
                    </div>
                  </div>
                </td>
              </tr>

              <!-- Delivery history panel -->
              <tr v-if="historyId === w.id">
                <td colspan="5" class="bg-gray-50 px-6 py-5 border-t border-gray-100">
                  <h3 class="text-sm font-semibold text-gray-700 mb-3">Recent Deliveries</h3>
                  <div v-if="loadingHistory" class="text-sm text-gray-400">Loading...</div>
                  <div v-else-if="deliveries.length === 0" class="text-sm text-gray-400">No delivery history.</div>
                  <table v-else class="w-full text-sm">
                    <thead class="text-left text-xs text-gray-500 uppercase">
                      <tr>
                        <th class="pb-2 pr-4">Time</th>
                        <th class="pb-2 pr-4">Event</th>
                        <th class="pb-2 pr-4">Status</th>
                        <th class="pb-2 pr-4">Attempts</th>
                        <th class="pb-2">Result</th>
                      </tr>
                    </thead>
                    <tbody class="divide-y divide-gray-100">
                      <tr v-for="d in deliveries" :key="d.id">
                        <td class="py-2 pr-4 text-gray-500 whitespace-nowrap">{{ formatDate(d.created_at) }}</td>
                        <td class="py-2 pr-4">
                          <span class="text-xs px-1.5 py-0.5 rounded"
                            :class="systemEvents.includes(d.event_type) ? 'bg-amber-50 text-amber-700' : d.event_type === 'test_ping' ? 'bg-gray-100 text-gray-600' : 'bg-blue-50 text-blue-700'">
                            {{ d.event_type }}
                          </span>
                        </td>
                        <td class="py-2 pr-4 text-gray-500">{{ d.status_code || '—' }}</td>
                        <td class="py-2 pr-4 text-gray-500">{{ d.attempts }}</td>
                        <td class="py-2">
                          <span class="text-xs font-medium px-2 py-0.5 rounded"
                            :class="d.success ? 'bg-green-50 text-green-700' : 'bg-red-50 text-red-600'">
                            {{ d.success ? 'OK' : (d.error_message || 'Failed') }}
                          </span>
                        </td>
                      </tr>
                    </tbody>
                  </table>
                </td>
              </tr>
            </template>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>
