<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import { api } from '../../api/client'
import type { Quota } from '../../types/api'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import InputNumber from 'primevue/inputnumber'
import Select from 'primevue/select'
import ProgressBar from 'primevue/progressbar'
import Dialog from 'primevue/dialog'
import ConfirmDialog from 'primevue/confirmdialog'

const confirm = useConfirm()
const toast = useToast()

const quotas = ref<Quota[]>([])
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
    toast.add({ severity: 'success', summary: 'Created', detail: 'Quota created', life: 3000 })
  } catch (e: any) {
    toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to create quota', life: 5000 })
  } finally {
    creating.value = false
  }
}

function deleteQuota(id: number) {
  confirm.require({
    message: 'Delete this quota?',
    header: 'Confirm Delete',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: async () => {
      try {
        await api.delete(`/api/v2/admin/quotas/${id}`)
        await fetchQuotas()
        toast.add({ severity: 'success', summary: 'Deleted', detail: 'Quota deleted', life: 3000 })
      } catch {
        toast.add({ severity: 'error', summary: 'Error', detail: 'Failed to delete quota', life: 5000 })
      }
    },
  })
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
    <ConfirmDialog />

    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold">Storage Quotas</h1>
      <Button icon="pi pi-plus" label="New Quota" @click="showCreate = !showCreate" />
    </div>

    <!-- Create dialog -->
    <Dialog v-model:visible="showCreate" header="New Quota" modal :style="{ width: '30rem' }">
      <div class="space-y-5">
        <div>
          <label class="text-sm font-medium block mb-1">Entity Type</label>
          <p class="text-xs text-surface-400 mb-2">What this quota applies to.</p>
          <Select v-model="newEntityType" :options="entityTypeOptions" optionLabel="label" optionValue="value" class="w-48" />
        </div>
        <div>
          <label class="text-sm font-medium block mb-1">Entity ID</label>
          <p class="text-xs text-surface-400 mb-2">Leave empty for system-wide quota.</p>
          <InputText v-model="newEntityId" placeholder="User/group ID" class="w-full" />
        </div>
        <div>
          <label class="text-sm font-medium block mb-1">Max Storage</label>
          <p class="text-xs text-surface-400 mb-2">Maximum storage allowed for this entity.</p>
          <div class="flex items-center gap-2">
            <InputNumber v-model="newMaxGB" :min="1" class="w-32" />
            <span class="text-sm text-surface-400">GB</span>
          </div>
        </div>
      </div>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="showCreate = false" />
        <Button :label="creating ? 'Creating...' : 'Create Quota'" :loading="creating" @click="createQuota" />
      </template>
    </Dialog>

    <!-- Quota list -->
    <DataTable :value="quotas" :loading="loading" class="rounded-lg border border-surface-200"
      :pt="{ root: { class: 'bg-surface-0' } }">
      <template #empty>
        No quotas configured. Quotas are optional -- create one to enforce storage limits.
      </template>
      <Column field="entity_type" header="Entity" sortable>
        <template #body="{ data }">
          <span class="font-medium">{{ data.entity_type }}</span>
          <span v-if="data.entity_id" class="text-surface-500 ml-1">({{ data.entity_id }})</span>
          <span v-if="!data.is_enabled" class="ml-2 text-xs text-surface-400">(disabled)</span>
        </template>
      </Column>
      <Column field="used_bytes" header="Usage" sortable style="min-width: 16rem">
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
          <Button icon="pi pi-trash" severity="danger" text rounded size="small" aria-label="Delete quota"
            v-tooltip.top="'Delete'" @click="deleteQuota(data.id)" />
        </template>
      </Column>
    </DataTable>
  </div>
</template>
