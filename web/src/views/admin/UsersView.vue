<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import { api } from '../../api/client'
import type { User } from '../../types/api'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import InputNumber from 'primevue/inputnumber'
import Select from 'primevue/select'
import Dialog from 'primevue/dialog'
import Tag from 'primevue/tag'
import Chip from 'primevue/chip'

const confirm = useConfirm()
const toast = useToast()

const users = ref<User[]>([])
const loading = ref(true)
const editingUser = ref<User | null>(null)
const showEditDialog = ref(false)
const editPermissions = ref('')
const editMaxFileSize = ref<number | null>(null)
const editAllowedTypes = ref<string[]>([])
const editAllowedTypesInput = ref('')
const saving = ref(false)

const showCreate = ref(false)
const newEmail = ref('')
const newPassword = ref('')
const newFirstName = ref('')
const newLastName = ref('')
const newPermissions = ref('read')
const creating = ref(false)

const permissionOptions = [
  { label: 'Read', value: 'read' },
  { label: 'Read + Write', value: 'read,write' },
  { label: 'Admin', value: 'admin,read,write' },
]

async function createUser() {
  creating.value = true
  try {
    await api.post('/api/v2/admin/users', {
      email: newEmail.value,
      password: newPassword.value,
      first_name: newFirstName.value,
      last_name: newLastName.value,
      permissions: newPermissions.value,
    })
    newEmail.value = ''
    newPassword.value = ''
    newFirstName.value = ''
    newLastName.value = ''
    newPermissions.value = 'read'
    showCreate.value = false
    await fetchUsers()
    toast.add({ severity: 'success', summary: 'Created', detail: 'User created', life: 3000 })
  } catch (e: any) {
    toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to create user', life: 5000 })
  } finally {
    creating.value = false
  }
}

async function fetchUsers() {
  loading.value = true
  try {
    const res = await api.get('/api/v2/admin/users')
    users.value = res.data.users || []
  } catch {
    // ignore
  } finally {
    loading.value = false
  }
}

function startEdit(u: User) {
  editingUser.value = u
  editPermissions.value = u.permissions || ''
  editMaxFileSize.value = u.max_file_size_bytes ?? null
  editAllowedTypes.value = [...(u.allowed_file_types || [])]
  editAllowedTypesInput.value = ''
  showEditDialog.value = true
}

function addAllowedType() {
  const v = editAllowedTypesInput.value.trim()
  if (!v) return
  if (!editAllowedTypes.value.includes(v)) {
    editAllowedTypes.value.push(v)
  }
  editAllowedTypesInput.value = ''
}

function removeAllowedType(idx: number) {
  editAllowedTypes.value.splice(idx, 1)
}

async function savePermissions() {
  if (!editingUser.value) return
  saving.value = true
  try {
    await api.put(`/api/v2/admin/users/${editingUser.value.id}/permissions`, {
      permissions: editPermissions.value,
    })
    // Restrictions ride on the main update endpoint, not the permissions one.
    await api.put(`/api/v2/admin/users/${editingUser.value.id}`, {
      max_file_size_bytes: editMaxFileSize.value,
      allowed_file_types: editAllowedTypes.value,
    })
    showEditDialog.value = false
    editingUser.value = null
    await fetchUsers()
    toast.add({ severity: 'success', summary: 'Updated', detail: 'User updated', life: 3000 })
  } catch (e: any) {
    toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to update user', life: 5000 })
  } finally {
    saving.value = false
  }
}

function deleteUser(id: number) {
  confirm.require({
    message: 'Delete this user? This cannot be undone.',
    header: 'Confirm Delete',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: async () => {
      try {
        await api.delete(`/api/v2/admin/users/${id}`)
        await fetchUsers()
        toast.add({ severity: 'success', summary: 'Deleted', detail: 'User deleted', life: 3000 })
      } catch (e: any) {
        toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to delete user', life: 5000 })
      }
    },
  })
}

function formatDate(iso: string) {
  const d = new Date(iso)
  const dd = String(d.getDate()).padStart(2, '0')
  const mm = String(d.getMonth() + 1).padStart(2, '0')
  const yyyy = d.getFullYear()
  const hh = String(d.getHours()).padStart(2, '0')
  const min = String(d.getMinutes()).padStart(2, '0')
  return `${dd}-${mm}-${yyyy} ${hh}:${min}`
}

function permissionSeverity(perms: string): string {
  if (perms?.includes('admin')) return 'danger'
  if (perms?.includes('write')) return 'info'
  return 'secondary'
}

onMounted(fetchUsers)
</script>

<template>
  <div class="p-6">

    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold">User Management</h1>
      <Button icon="pi pi-plus" label="Add User" @click="showCreate = !showCreate" />
    </div>

    <!-- Create user dialog -->
    <Dialog v-model:visible="showCreate" header="Add User" modal :style="{ width: '30rem' }">
      <div class="space-y-5">
        <div>
          <label class="text-sm font-medium block mb-1">First Name</label>
          <InputText v-model="newFirstName" required class="w-full" />
        </div>
        <div>
          <label class="text-sm font-medium block mb-1">Last Name</label>
          <InputText v-model="newLastName" required class="w-full" />
        </div>
        <div>
          <label class="text-sm font-medium block mb-1">Email</label>
          <InputText v-model="newEmail" type="email" required class="w-full" />
        </div>
        <div>
          <label class="text-sm font-medium block mb-1">Password</label>
          <p class="text-xs text-surface-400 mb-2">Minimum 8 characters.</p>
          <InputText v-model="newPassword" type="password" required class="w-full" />
        </div>
        <div>
          <label class="text-sm font-medium block mb-1">Permissions</label>
          <p class="text-xs text-surface-400 mb-2">Access level for this user.</p>
          <Select v-model="newPermissions" :options="permissionOptions" optionLabel="label" optionValue="value" class="w-48" />
        </div>
      </div>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="showCreate = false" />
        <Button :label="creating ? 'Creating...' : 'Create User'" :loading="creating" @click="createUser" />
      </template>
    </Dialog>

    <!-- Edit user dialog (permissions + restrictions) -->
    <Dialog v-model:visible="showEditDialog" modal :header="editingUser ? `Edit User: ${editingUser.first_name} ${editingUser.last_name}` : ''" class="w-full max-w-lg">
      <div class="grid grid-cols-3 gap-6 py-2">
        <div>
          <label class="text-sm font-medium">Permissions</label>
          <p class="text-xs text-surface-400 mt-1">Access level for this user.</p>
        </div>
        <div class="col-span-2">
          <Select v-model="editPermissions" :options="permissionOptions" optionLabel="label" optionValue="value" class="w-48" />
        </div>

        <div>
          <label class="text-sm font-medium">Max upload size</label>
          <p class="text-xs text-surface-400 mt-1">Bytes. Leave empty to use the system default.</p>
        </div>
        <div class="col-span-2">
          <InputNumber v-model="editMaxFileSize" :min="0" placeholder="Use system default" class="w-full" />
        </div>

        <div>
          <label class="text-sm font-medium">Allowed file types</label>
          <p class="text-xs text-surface-400 mt-1">Content type patterns (e.g. <code>image/*</code>, <code>application/pdf</code>). Empty = use system default.</p>
        </div>
        <div class="col-span-2 space-y-2">
          <div class="flex gap-2">
            <InputText v-model="editAllowedTypesInput" placeholder="image/* or application/pdf" class="flex-1" @keydown.enter.prevent="addAllowedType" />
            <Button icon="pi pi-plus" severity="secondary" @click="addAllowedType" />
          </div>
          <div class="flex flex-wrap gap-2">
            <Chip v-for="(t, idx) in editAllowedTypes" :key="t" :label="t" removable @remove="removeAllowedType(idx)" />
          </div>
        </div>
      </div>
      <template #footer>
        <div class="flex justify-end gap-2">
          <Button label="Cancel" severity="secondary" outlined @click="showEditDialog = false" />
          <Button :label="saving ? 'Saving...' : 'Save'" :loading="saving" @click="savePermissions" />
        </div>
      </template>
    </Dialog>

    <!-- Users table -->
    <DataTable :value="users" :loading="loading" stripedRows class="rounded-lg border border-surface-200"
      :pt="{ root: { class: 'bg-surface-0' } }">
      <template #empty>No users found.</template>
      <Column field="name" header="Name" sortable>
        <template #body="{ data }">
          <span class="font-medium">{{ data.first_name }} {{ data.last_name }}</span>
          <span v-if="data.is_service_account" class="ml-1 text-xs text-surface-400">(service)</span>
        </template>
      </Column>
      <Column field="email" header="Email" sortable>
        <template #body="{ data }">
          <span class="text-surface-500">{{ data.email }}</span>
        </template>
      </Column>
      <Column field="permissions" header="Permissions" sortable>
        <template #body="{ data }">
          <Tag :value="data.permissions || 'read'" :severity="permissionSeverity(data.permissions)" />
        </template>
      </Column>
      <Column field="is_active" header="Status" sortable>
        <template #body="{ data }">
          <Tag :value="data.is_active ? 'Active' : 'Disabled'" :severity="data.is_active ? 'success' : 'danger'" />
        </template>
      </Column>
      <Column field="created_at" header="Joined" sortable>
        <template #body="{ data }">
          <span class="text-surface-400">{{ formatDate(data.created_at) }}</span>
        </template>
      </Column>
      <Column header="Actions">
        <template #body="{ data }">
          <div class="flex gap-2">
            <Button icon="pi pi-pencil" severity="info" text rounded size="small" aria-label="Edit permissions"
              v-tooltip.top="'Edit'" @click="startEdit(data)" />
            <Button icon="pi pi-trash" severity="danger" text rounded size="small" aria-label="Delete user"
              v-tooltip.top="'Delete'" @click="deleteUser(data.id)" />
          </div>
        </template>
      </Column>
    </DataTable>
  </div>
</template>
