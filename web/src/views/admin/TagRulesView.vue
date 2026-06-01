<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import { api } from '../../api/client'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import InputNumber from 'primevue/inputnumber'
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import ToggleSwitch from 'primevue/toggleswitch'
import Dialog from 'primevue/dialog'

interface TagRule {
  id: number
  name: string
  description: string
  match_field: string
  match_op: string
  match_value: string
  apply_key: string
  apply_value: string
  priority: number
  is_enabled: boolean
  created_at: string
  updated_at: string
}

const confirm = useConfirm()
const toast = useToast()

const rules = ref<TagRule[]>([])
const loading = ref(true)

// Dialog state
const showDialog = ref(false)
const editingId = ref<number | null>(null)
const saving = ref(false)

// Form fields
const formName = ref('')
const formDescription = ref('')
const formMatchField = ref('content_type')
const formMatchOp = ref('equals')
const formMatchValue = ref('')
const formApplyKey = ref('')
const formApplyValue = ref('')
const formPriority = ref(100)

const matchFieldOptions = [
  { label: 'Content Type', value: 'content_type' },
  { label: 'Filename', value: 'filename' },
  { label: 'File Size', value: 'file_size' },
  { label: 'Visibility', value: 'visibility' },
]

const operatorsByField: Record<string, { label: string; value: string }[]> = {
  content_type: [
    { label: 'equals', value: 'equals' },
    { label: 'contains', value: 'contains' },
    { label: 'regex', value: 'regex' },
  ],
  filename: [
    { label: 'equals', value: 'equals' },
    { label: 'contains', value: 'contains' },
    { label: 'regex', value: 'regex' },
  ],
  file_size: [
    { label: 'greater than', value: 'gt' },
    { label: 'less than', value: 'lt' },
  ],
  visibility: [
    { label: 'equals', value: 'equals' },
  ],
}

const matchOpOptions = computed(() => {
  return operatorsByField[formMatchField.value] || []
})

function formatMatchExpr(rule: TagRule): string {
  const fieldLabels: Record<string, string> = {
    content_type: 'content type',
    filename: 'filename',
    file_size: 'file size',
    visibility: 'visibility',
  }
  const opLabels: Record<string, string> = {
    equals: '=',
    contains: 'contains',
    regex: 'matches',
    gt: '>',
    lt: '<',
  }
  const field = fieldLabels[rule.match_field] || rule.match_field
  const op = opLabels[rule.match_op] || rule.match_op
  return `${field} ${op} ${rule.match_value}`
}

function resetForm() {
  formName.value = ''
  formDescription.value = ''
  formMatchField.value = 'content_type'
  formMatchOp.value = 'equals'
  formMatchValue.value = ''
  formApplyKey.value = ''
  formApplyValue.value = ''
  formPriority.value = 100
}

function openCreate() {
  editingId.value = null
  resetForm()
  showDialog.value = true
}

function openEdit(rule: TagRule) {
  editingId.value = rule.id
  formName.value = rule.name
  formDescription.value = rule.description
  formMatchField.value = rule.match_field
  formMatchOp.value = rule.match_op
  formMatchValue.value = rule.match_value
  formApplyKey.value = rule.apply_key
  formApplyValue.value = rule.apply_value
  formPriority.value = rule.priority
  showDialog.value = true
}

function onMatchFieldChange() {
  const opts = operatorsByField[formMatchField.value] || []
  const valid = opts.some((o) => o.value === formMatchOp.value)
  if (!valid && opts.length > 0) {
    formMatchOp.value = opts[0].value
  }
}

async function fetchRules() {
  loading.value = true
  try {
    const res = await api.get('/api/v2/admin/tag-rules')
    rules.value = res.data.rules || []
  } catch {
    // ignore
  } finally {
    loading.value = false
  }
}

async function saveRule() {
  saving.value = true
  const payload = {
    name: formName.value,
    description: formDescription.value,
    match_field: formMatchField.value,
    match_op: formMatchOp.value,
    match_value: formMatchValue.value,
    apply_key: formApplyKey.value,
    apply_value: formApplyValue.value,
    priority: formPriority.value,
  }
  try {
    if (editingId.value) {
      await api.put(`/api/v2/admin/tag-rules/${editingId.value}`, payload)
      toast.add({ severity: 'success', summary: 'Updated', detail: 'Rule updated', life: 3000 })
    } else {
      await api.post('/api/v2/admin/tag-rules', payload)
      toast.add({ severity: 'success', summary: 'Created', detail: 'Rule created', life: 3000 })
    }
    showDialog.value = false
    await fetchRules()
  } catch (e: any) {
    toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to save rule', life: 5000 })
  } finally {
    saving.value = false
  }
}

async function toggleEnabled(rule: TagRule) {
  try {
    await api.put(`/api/v2/admin/tag-rules/${rule.id}`, {
      name: rule.name,
      description: rule.description,
      match_field: rule.match_field,
      match_op: rule.match_op,
      match_value: rule.match_value,
      apply_key: rule.apply_key,
      apply_value: rule.apply_value,
      priority: rule.priority,
      is_enabled: !rule.is_enabled,
    })
    await fetchRules()
  } catch {
    toast.add({ severity: 'error', summary: 'Error', detail: 'Failed to update rule', life: 5000 })
  }
}

function deleteRule(rule: TagRule) {
  confirm.require({
    message: `Delete rule "${rule.name}"? This cannot be undone.`,
    header: 'Confirm Delete',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: async () => {
      try {
        await api.delete(`/api/v2/admin/tag-rules/${rule.id}`)
        toast.add({ severity: 'success', summary: 'Deleted', detail: 'Rule deleted', life: 3000 })
        await fetchRules()
      } catch (e: any) {
        toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to delete rule', life: 5000 })
      }
    },
  })
}

const dialogHeader = computed(() => editingId.value ? 'Edit Rule' : 'New Rule')

onMounted(fetchRules)
</script>

<template>
  <div class="p-6">

    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold">Auto-Tag Rules</h1>
      <Button label="New Rule" icon="pi pi-plus" @click="openCreate" />
    </div>

    <!-- Create / Edit dialog -->
    <Dialog v-model:visible="showDialog" :header="dialogHeader" modal :style="{ width: '36rem' }">
      <div class="space-y-5">
        <div>
          <label class="text-sm font-medium block mb-1">Name</label>
          <InputText v-model="formName" placeholder="e.g. Tag PDFs as document" class="w-full" />
        </div>
        <div>
          <label class="text-sm font-medium block mb-1">Description</label>
          <InputText v-model="formDescription" placeholder="Optional description" class="w-full" />
        </div>

        <!-- Match criteria -->
        <div class="grid grid-cols-2 gap-4">
          <div>
            <label class="text-sm font-medium block mb-1">Match Field</label>
            <Select v-model="formMatchField" :options="matchFieldOptions" optionLabel="label" optionValue="value" class="w-full" @change="onMatchFieldChange" />
          </div>
          <div>
            <label class="text-sm font-medium block mb-1">Match Operator</label>
            <Select v-model="formMatchOp" :options="matchOpOptions" optionLabel="label" optionValue="value" class="w-full" />
          </div>
        </div>
        <div>
          <label class="text-sm font-medium block mb-1">Match Value</label>
          <InputText v-model="formMatchValue" placeholder="e.g. application/pdf" class="w-full font-mono" />
        </div>

        <!-- Apply tag -->
        <div class="grid grid-cols-2 gap-4">
          <div>
            <label class="text-sm font-medium block mb-1">Apply Key</label>
            <InputText v-model="formApplyKey" placeholder="e.g. type" class="w-full" />
          </div>
          <div>
            <label class="text-sm font-medium block mb-1">Apply Value</label>
            <InputText v-model="formApplyValue" placeholder="e.g. document" class="w-full" />
          </div>
        </div>

        <div>
          <label class="text-sm font-medium block mb-1">Priority</label>
          <p class="text-xs text-surface-400 mb-2">Lower numbers run first.</p>
          <InputNumber v-model="formPriority" :min="0" class="w-32" />
        </div>
      </div>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="showDialog = false" />
        <Button :label="saving ? 'Saving...' : (editingId ? 'Save Changes' : 'Create Rule')" :loading="saving" @click="saveRule" />
      </template>
    </Dialog>

    <!-- Rules table -->
    <DataTable :value="rules" :loading="loading" stripedRows class="rounded-lg border border-surface-200"
      :pt="{ root: { class: 'bg-surface-0' } }">
      <template #empty>No auto-tag rules configured. Click "New Rule" to create one.</template>
      <Column field="name" header="Name" sortable>
        <template #body="{ data }">
          <div>
            <span class="font-medium">{{ data.name }}</span>
            <p v-if="data.description" class="text-xs text-surface-400 mt-0.5">{{ data.description }}</p>
          </div>
        </template>
      </Column>
      <Column header="Match" sortable sortField="match_field">
        <template #body="{ data }">
          <span class="text-sm font-mono text-surface-500">{{ formatMatchExpr(data) }}</span>
        </template>
      </Column>
      <Column header="Apply">
        <template #body="{ data }">
          <Tag :value="`${data.apply_key}:${data.apply_value}`" severity="info" />
        </template>
      </Column>
      <Column field="priority" header="Priority" sortable style="width: 7rem">
        <template #body="{ data }">
          <span class="text-surface-500">{{ data.priority }}</span>
        </template>
      </Column>
      <Column field="is_enabled" header="Enabled" sortable style="width: 7rem">
        <template #body="{ data }">
          <ToggleSwitch :modelValue="data.is_enabled" @update:modelValue="toggleEnabled(data)" />
        </template>
      </Column>
      <Column header="Actions" style="width: 8rem">
        <template #body="{ data }">
          <div class="flex items-center gap-1">
            <Button icon="pi pi-pencil" severity="secondary" text rounded size="small"
              aria-label="Edit" v-tooltip.top="'Edit'" @click="openEdit(data)" />
            <Button icon="pi pi-trash" severity="danger" text rounded size="small"
              aria-label="Delete" v-tooltip.top="'Delete'" @click="deleteRule(data)" />
          </div>
        </template>
      </Column>
    </DataTable>
  </div>
</template>
