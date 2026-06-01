<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import { api } from '../../api/client'
import type { Quota, User, Group } from '../../types/api'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import InputNumber from 'primevue/inputnumber'
import Select from 'primevue/select'
import ProgressBar from 'primevue/progressbar'
import Dialog from 'primevue/dialog'

const confirm = useConfirm()
const toast = useToast()

const quotas = ref<Quota[]>([])
const loading = ref(true)
const showCreate = ref(false)
const newEntityType = ref('system')
// Holds a user/group id (number, via the picker) or a department label
// (string, via the editable dropdown). Coerced to a string when posted.
const newEntityId = ref<string | number | null>(null)
const newMaxValue = ref<number>(10)
const newUnit = ref<'MB' | 'GB'>('GB')
const creating = ref(false)

const allUsers = ref<User[]>([])
const allGroups = ref<Group[]>([])
const departments = ref<string[]>([])

const entityTypeOptions = [
  { label: 'System', value: 'system' },
  { label: 'User', value: 'user' },
  { label: 'Group', value: 'group' },
  { label: 'Department', value: 'department' },
]

const unitOptions = [
  { label: 'MB', value: 'MB' },
  { label: 'GB', value: 'GB' },
]

const UNIT_BYTES: Record<string, number> = { MB: 1048576, GB: 1073741824 }

// Picker options. Showing the user's email / group name keeps admins from
// having to know the numeric id (V2-396).
const userOptions = computed(() => allUsers.value.map(u => ({ value: u.id, label: u.email })))
const groupOptions = computed(() => allGroups.value.map(g => ({ value: g.id, label: g.name })))

// A system quota applies to everyone, so it needs no entity. Everything else
// requires one — otherwise the row matches nothing and enforces nothing (V2-397).
const requiresEntity = computed(() => newEntityType.value !== 'system')
const entityMissing = computed(
  () => requiresEntity.value && (newEntityId.value === null || newEntityId.value === ''),
)
const canCreate = computed(() => !entityMissing.value && (newMaxValue.value || 0) > 0)

// Reset the entity selection when the type changes — a user id is meaningless
// once the type flips to group/department.
watch(newEntityType, () => {
  newEntityId.value = null
})

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

// Populate the entity pickers. Each is best-effort: a failed fetch just leaves
// that dropdown empty rather than blocking the dialog.
async function fetchPickerData() {
  try {
    allUsers.value = (await api.get('/api/v2/admin/users')).data.users || []
  } catch { /* empty user picker */ }
  try {
    allGroups.value = (await api.get('/api/v2/admin/groups')).data.groups || []
  } catch { /* empty group picker */ }
  try {
    departments.value = (await api.get('/api/v2/admin/departments')).data.departments || []
  } catch { /* empty department suggestions */ }
}

async function createQuota() {
  if (!canCreate.value) return
  creating.value = true
  try {
    await api.post('/api/v2/admin/quotas', {
      entity_type: newEntityType.value,
      entity_id: requiresEntity.value ? String(newEntityId.value ?? '') : undefined,
      max_bytes: Math.round((newMaxValue.value || 0) * UNIT_BYTES[newUnit.value]),
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

onMounted(() => {
  fetchQuotas()
  fetchPickerData()
})
</script>

<template>
  <div class="p-6">

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
        <div v-if="newEntityType === 'system'" class="text-xs text-surface-400">
          A system quota applies to all storage across every user.
        </div>
        <div v-else>
          <label class="text-sm font-medium block mb-1">
            {{ newEntityType === 'user' ? 'User' : newEntityType === 'group' ? 'Group' : 'Department' }}
          </label>
          <p class="text-xs text-surface-400 mb-2">
            {{ newEntityType === 'department'
              ? 'Pick a known department or type a new one.'
              : `Choose the ${newEntityType} this quota applies to.` }}
          </p>
          <Select v-if="newEntityType === 'user'" v-model="newEntityId" :options="userOptions"
            optionLabel="label" optionValue="value" filter placeholder="Select a user" class="w-full" />
          <Select v-else-if="newEntityType === 'group'" v-model="newEntityId" :options="groupOptions"
            optionLabel="label" optionValue="value" filter placeholder="Select a group" class="w-full" />
          <Select v-else v-model="newEntityId" :options="departments" editable
            placeholder="Department name" class="w-full" />
        </div>
        <div>
          <label class="text-sm font-medium block mb-1">Max Storage</label>
          <p class="text-xs text-surface-400 mb-2">Maximum total stored data for this entity — not a file count.</p>
          <div class="flex items-center gap-2">
            <InputNumber v-model="newMaxValue" :min="1" class="w-32" />
            <Select v-model="newUnit" :options="unitOptions" optionLabel="label" optionValue="value" class="w-24" />
          </div>
        </div>
      </div>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="showCreate = false" />
        <Button :label="creating ? 'Creating...' : 'Create Quota'" :loading="creating" :disabled="!canCreate" @click="createQuota" />
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
