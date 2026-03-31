<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '../../api/client'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Dialog from 'primevue/dialog'
import Tag from 'primevue/tag'

const users = ref<any[]>([])
const loading = ref(true)
const editingUser = ref<any>(null)
const editPermissions = ref('')
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
  } catch (e: any) {
    alert(e.response?.data?.error || 'Failed to create user')
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

function startEdit(u: any) {
  editingUser.value = u
  editPermissions.value = u.permissions || ''
}

async function savePermissions() {
  if (!editingUser.value) return
  saving.value = true
  try {
    await api.put(`/api/v2/admin/users/${editingUser.value.id}/permissions`, {
      permissions: editPermissions.value,
    })
    editingUser.value = null
    await fetchUsers()
  } catch (e: any) {
    alert(e.response?.data?.error || 'Failed to update permissions')
  } finally {
    saving.value = false
  }
}

async function deleteUser(id: number) {
  if (!confirm('Delete this user? This cannot be undone.')) return
  try {
    await api.delete(`/api/v2/admin/users/${id}`)
    await fetchUsers()
  } catch (e: any) {
    alert(e.response?.data?.error || 'Failed to delete user')
  }
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

    <!-- Create user form -->
    <div v-if="showCreate" class="bg-surface-0 rounded-lg border border-surface-200 mb-6">
      <div class="px-6 py-4 border-b border-surface-200">
        <h2 class="text-base font-semibold">Create User</h2>
      </div>
      <form @submit.prevent="createUser">
        <div class="divide-y divide-surface-100">
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium">First Name</label>
            </div>
            <div class="col-span-2">
              <InputText v-model="newFirstName" required class="w-full max-w-md" />
            </div>
          </div>
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium">Last Name</label>
            </div>
            <div class="col-span-2">
              <InputText v-model="newLastName" required class="w-full max-w-md" />
            </div>
          </div>
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium">Email</label>
            </div>
            <div class="col-span-2">
              <InputText v-model="newEmail" type="email" required class="w-full max-w-md" />
            </div>
          </div>
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium">Password</label>
              <p class="text-xs text-surface-400 mt-1">Minimum 8 characters.</p>
            </div>
            <div class="col-span-2">
              <InputText v-model="newPassword" type="password" required class="w-full max-w-md" />
            </div>
          </div>
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium">Permissions</label>
              <p class="text-xs text-surface-400 mt-1">Access level for this user.</p>
            </div>
            <div class="col-span-2">
              <Select v-model="newPermissions" :options="permissionOptions" optionLabel="label" optionValue="value" class="w-48" />
            </div>
          </div>
        </div>
        <div class="px-6 py-4 bg-surface-50 border-t border-surface-200 rounded-b-lg flex justify-end gap-2">
          <Button type="button" label="Cancel" severity="secondary" outlined @click="showCreate = false" />
          <Button type="submit" :label="creating ? 'Creating...' : 'Create User'" :loading="creating" />
        </div>
      </form>
    </div>

    <!-- Edit permissions dialog -->
    <Dialog v-model:visible="editingUser" modal :header="editingUser ? `Edit Permissions: ${editingUser.first_name} ${editingUser.last_name}` : ''" class="w-full max-w-lg">
      <div class="grid grid-cols-3 gap-6 py-2">
        <div>
          <label class="text-sm font-medium">Permissions</label>
          <p class="text-xs text-surface-400 mt-1">Access level for this user.</p>
        </div>
        <div class="col-span-2">
          <Select v-model="editPermissions" :options="permissionOptions" optionLabel="label" optionValue="value" class="w-48" />
        </div>
      </div>
      <template #footer>
        <div class="flex justify-end gap-2">
          <Button label="Cancel" severity="secondary" outlined @click="editingUser = null" />
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
            <Button icon="pi pi-pencil" severity="info" text rounded size="small" @click="startEdit(data)" />
            <Button icon="pi pi-trash" severity="danger" text rounded size="small" @click="deleteUser(data.id)" />
          </div>
        </template>
      </Column>
    </DataTable>
  </div>
</template>
