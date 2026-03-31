<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '../../api/client'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import InputNumber from 'primevue/inputnumber'
import Select from 'primevue/select'
import ProgressBar from 'primevue/progressbar'

const quotas = ref<any[]>([])
const loading = ref(true)
const showCreate = ref(false)
const newEntityType = ref('system')
const newEntityId = ref('')
const newMaxGB = ref<number>(10)
const creating = ref(false)

const entityTypeOptions = [
  { label: 'System', value: 'system' },
  { label: 'User', value: 'user' },
  { label: 'Group', value: 'group' },
  { label: 'Department', value: 'department' },
]

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
      max_bytes: (newMaxGB.value || 0) * 1073741824,
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
  return Math.min(100, Math.round((used / max) * 100))
}

function usageSeverity(used: number, max: number): string | undefined {
  const pct = usagePct(used, max)
  if (pct > 90) return 'danger'
  if (pct > 70) return 'warn'
  return undefined
}

onMounted(fetchQuotas)
</script>

<template>
  <div class="p-6">
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold">Storage Quotas</h1>
      <Button icon="pi pi-plus" label="New Quota" @click="showCreate = !showCreate" />
    </div>

    <!-- Create form -->
    <div v-if="showCreate" class="bg-surface-0 rounded-lg border border-surface-200 mb-6">
      <div class="px-6 py-4 border-b border-surface-200">
        <h2 class="text-base font-semibold">New Quota</h2>
      </div>
      <form @submit.prevent="createQuota">
        <div class="divide-y divide-surface-100">
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium">Entity Type</label>
              <p class="text-xs text-surface-400 mt-1">What this quota applies to.</p>
            </div>
            <div class="col-span-2">
              <Select v-model="newEntityType" :options="entityTypeOptions" optionLabel="label" optionValue="value" class="w-48" />
            </div>
          </div>
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium">Entity ID</label>
              <p class="text-xs text-surface-400 mt-1">Leave empty for system-wide quota.</p>
            </div>
            <div class="col-span-2">
              <InputText v-model="newEntityId" placeholder="User/group ID" class="w-full max-w-md" />
            </div>
          </div>
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium">Max Storage</label>
              <p class="text-xs text-surface-400 mt-1">Maximum storage allowed for this entity.</p>
            </div>
            <div class="col-span-2">
              <div class="flex items-center gap-2">
                <InputNumber v-model="newMaxGB" :min="1" class="w-32" />
                <span class="text-sm text-surface-400">GB</span>
              </div>
            </div>
          </div>
        </div>
        <div class="px-6 py-4 bg-surface-50 border-t border-surface-200 rounded-b-lg flex justify-end gap-2">
          <Button type="button" label="Cancel" severity="secondary" outlined @click="showCreate = false" />
          <Button type="submit" :label="creating ? 'Creating...' : 'Create Quota'" :loading="creating" />
        </div>
      </form>
    </div>

    <!-- Quota list -->
    <DataTable :value="quotas" :loading="loading" class="rounded-lg border border-surface-200"
      :pt="{ root: { class: 'bg-surface-0' } }">
      <template #empty>
        No quotas configured. Quotas are optional -- create one to enforce storage limits.
      </template>
      <Column field="entity_type" header="Entity">
        <template #body="{ data }">
          <span class="font-medium">{{ data.entity_type }}</span>
          <span v-if="data.entity_id" class="text-surface-500 ml-1">({{ data.entity_id }})</span>
          <span v-if="!data.is_enabled" class="ml-2 text-xs text-surface-400">(disabled)</span>
        </template>
      </Column>
      <Column header="Usage" style="min-width: 16rem">
        <template #body="{ data }">
          <div class="flex flex-col gap-1">
            <div class="flex justify-between text-xs text-surface-500">
              <span>{{ formatBytes(data.used_bytes) }}</span>
              <span>{{ formatBytes(data.max_bytes) }}</span>
            </div>
            <ProgressBar :value="usagePct(data.used_bytes, data.max_bytes)" :showValue="false"
              :severity="usageSeverity(data.used_bytes, data.max_bytes)" style="height: 0.5rem" />
          </div>
        </template>
      </Column>
      <Column header="Actions" style="width: 5rem">
        <template #body="{ data }">
          <Button icon="pi pi-trash" severity="danger" text rounded size="small" @click="deleteQuota(data.id)" />
        </template>
      </Column>
    </DataTable>
  </div>
</template>
