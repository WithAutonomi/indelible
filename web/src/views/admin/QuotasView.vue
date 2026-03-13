<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '../../api/client'

const quotas = ref<any[]>([])
const loading = ref(true)
const showCreate = ref(false)
const newEntityType = ref('system')
const newEntityId = ref('')
const newMaxGB = ref(10)
const creating = ref(false)

async function fetchQuotas() {
  loading.value = true
  try {
    const res = await api.get('/api/v2/admin/quotas')
    quotas.value = res.data.quotas || []
  } catch {
    // ignore
  } finally {
    loading.value = false
  }
}

async function createQuota() {
  creating.value = true
  try {
    await api.post('/api/v2/admin/quotas', {
      entity_type: newEntityType.value,
      entity_id: newEntityId.value || undefined,
      max_bytes: newMaxGB.value * 1073741824,
    })
    showCreate.value = false
    await fetchQuotas()
  } catch (e: any) {
    alert(e.response?.data?.error || 'Failed to create quota')
  } finally {
    creating.value = false
  }
}

async function deleteQuota(id: number) {
  if (!confirm('Delete this quota?')) return
  try {
    await api.delete(`/api/v2/admin/quotas/${id}`)
    await fetchQuotas()
  } catch {
    alert('Failed to delete quota')
  }
}

function formatBytes(bytes: number) {
  if (!bytes) return '0 B'
  if (bytes < 1073741824) return (bytes / 1048576).toFixed(1) + ' MB'
  return (bytes / 1073741824).toFixed(2) + ' GB'
}

function usagePct(used: number, max: number) {
  if (!max) return 0
  return Math.min(100, (used / max) * 100)
}

onMounted(fetchQuotas)
</script>

<template>
  <div class="p-6 max-w-4xl">
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold">Storage Quotas</h1>
      <button @click="showCreate = !showCreate"
        class="rounded bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700">
        <i class="pi pi-plus mr-1"></i> New Quota
      </button>
    </div>

    <!-- Create form -->
    <div v-if="showCreate" class="bg-white rounded-lg border border-gray-200 p-6 mb-6">
      <form @submit.prevent="createQuota" class="space-y-4">
        <div class="grid grid-cols-2 gap-4">
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">Entity Type</label>
            <select v-model="newEntityType" class="block w-full rounded border border-gray-300 px-3 py-2 text-sm">
              <option value="system">System</option>
              <option value="user">User</option>
              <option value="group">Group</option>
              <option value="department">Department</option>
            </select>
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">Entity ID</label>
            <input v-model="newEntityId" type="text" placeholder="User/group ID (empty for system)"
              class="block w-full rounded border border-gray-300 px-3 py-2 text-sm" />
          </div>
        </div>
        <div>
          <label class="block text-sm font-medium text-gray-700 mb-1">Max Storage (GB)</label>
          <input v-model.number="newMaxGB" type="number" min="1" required
            class="block w-full rounded border border-gray-300 px-3 py-2 text-sm" />
        </div>
        <button type="submit" :disabled="creating"
          class="rounded bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700 disabled:opacity-50">
          {{ creating ? 'Creating...' : 'Create Quota' }}
        </button>
      </form>
    </div>

    <!-- Quota list -->
    <div class="bg-white rounded-lg border border-gray-200">
      <div v-if="loading" class="p-6 text-center text-gray-400">Loading...</div>
      <div v-else-if="quotas.length === 0" class="p-6 text-center text-gray-400">
        No quotas configured. Quotas are optional — create one to enforce storage limits.
      </div>
      <div v-else class="divide-y divide-gray-100">
        <div v-for="q in quotas" :key="q.id" class="px-6 py-4">
          <div class="flex items-center justify-between mb-2">
            <div>
              <span class="text-sm font-medium text-gray-800">{{ q.entity_type }}</span>
              <span v-if="q.entity_id" class="text-sm text-gray-500 ml-1">({{ q.entity_id }})</span>
              <span v-if="!q.is_enabled" class="ml-2 text-xs text-gray-400">(disabled)</span>
            </div>
            <div class="flex items-center gap-3">
              <span class="text-sm text-gray-500">{{ formatBytes(q.used_bytes) }} / {{ formatBytes(q.max_bytes) }}</span>
              <button @click="deleteQuota(q.id)" class="text-red-500 hover:text-red-700">
                <i class="pi pi-trash text-sm"></i>
              </button>
            </div>
          </div>
          <div class="w-full bg-gray-100 rounded-full h-2">
            <div class="h-2 rounded-full transition-all"
              :class="usagePct(q.used_bytes, q.max_bytes) > 90 ? 'bg-red-500' : usagePct(q.used_bytes, q.max_bytes) > 70 ? 'bg-yellow-500' : 'bg-blue-500'"
              :style="{ width: usagePct(q.used_bytes, q.max_bytes) + '%' }">
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
