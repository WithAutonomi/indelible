<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
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
import Drawer from 'primevue/drawer'

const confirm = useConfirm()
const toast = useToast()
const route = useRoute()

const users = ref<User[]>([])
const loading = ref(true)
const editingUser = ref<User | null>(null)
const showEditDialog = ref(false)
const editPermissions = ref('')
const editMaxFileSize = ref<number | null>(null)
const editAllowedTypes = ref<string[]>([])
const editAllowedTypesInput = ref('')
const saving = ref(false)

// Read-only user details drawer. Related content (group memberships, the user's
// API tokens, quota usage, SSO identity) is a follow-up — those need new read
// endpoints; this surfaces what the list response already carries.
const detailVisible = ref(false)
const detailUser = ref<User | null>(null)
function openDetail(u: User) {
  detailUser.value = u
  detailVisible.value = true
}
function editFromDetail() {
  if (!detailUser.value) return
  const u = detailUser.value
  detailVisible.value = false
  startEdit(u)
}

function formatBytes(n: number | null | undefined): string {
  if (n == null) return 'System default'
  if (n === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++ }
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${units[i]}`
}
function fmtDateTime(s?: string | null): string {
  return s ? new Date(s).toLocaleString() : 'Never'
}
async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    toast.add({ severity: 'success', summary: 'Copied', detail: text, life: 2000 })
  } catch {
    toast.add({ severity: 'warn', summary: 'Copy failed', life: 2000 })
  }
}

const showCreate = ref(false)
const newEmail = ref('')
const newPassword = ref('')
const newFirstName = ref('')
const newLastName = ref('')
const newPermissions = ref('read')
const creating = ref(false)

// The backend stores a single permission level (read | write | admin) — the
// hierarchy admin > write > read is applied server-side. These option values
// must be those bare levels: composite values like "read,write" never matched
// a user's stored level, so the edit Select rendered blank and Create wrote an
// invalid level string.
const permissionOptions = [
  { label: 'Read', value: 'read' },
  { label: 'Read + Write', value: 'write' },
  { label: 'Admin', value: 'admin' },
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

// V2-459: a global-search result can deep-link here with ?focus=<id> to open
// that user's details drawer. Fetched directly so it works regardless of which
// page of the list the user is on.
async function openFocused(id: string) {
  try {
    const u = (await api.get(`/api/v2/admin/users/${id}`)).data
    if (u && u.id) openDetail(u)
  } catch {
    // user may have been deleted — leave the list as-is.
  }
}

onMounted(async () => {
  await fetchUsers()
  if (route.query.focus) openFocused(route.query.focus as string)
})

watch(() => route.query.focus, (f, old) => {
  if (f && f !== old) openFocused(f as string)
})
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
          <button type="button" class="font-medium hover:text-primary hover:underline text-left"
            @click="openDetail(data)" v-tooltip.top="'View details'">
            {{ data.first_name }} {{ data.last_name }}
          </button>
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

    <!-- User details (read-only). Groups / tokens / quota are a tracked follow-up. -->
    <Drawer v-model:visible="detailVisible" position="right" :style="{ width: '26rem' }"
      :header="detailUser ? `${detailUser.first_name} ${detailUser.last_name}` : 'User'">
      <div v-if="detailUser" class="flex flex-col gap-5 text-sm">
        <section>
          <h3 class="text-xs font-semibold uppercase text-surface-400 mb-2">Identity</h3>
          <dl class="flex flex-col gap-2">
            <div class="flex justify-between gap-3 items-center">
              <dt class="text-surface-500 shrink-0">Email</dt>
              <dd class="flex items-center gap-1 min-w-0">
                <span class="truncate">{{ detailUser.email }}</span>
                <Button icon="pi pi-copy" text rounded size="small" @click="copyText(detailUser.email)" v-tooltip.left="'Copy'" />
              </dd>
            </div>
            <div class="flex justify-between gap-3"><dt class="text-surface-500">User ID</dt><dd class="font-mono text-xs">{{ detailUser.id }}</dd></div>
            <div v-if="detailUser.is_service_account" class="flex justify-between gap-3"><dt class="text-surface-500">Type</dt><dd>Service account</dd></div>
          </dl>
        </section>

        <section>
          <h3 class="text-xs font-semibold uppercase text-surface-400 mb-2">Access</h3>
          <dl class="flex flex-col gap-2">
            <div class="flex justify-between gap-3"><dt class="text-surface-500">Permissions</dt><dd><Tag :value="detailUser.permissions || 'read'" :severity="permissionSeverity(detailUser.permissions)" /></dd></div>
            <div class="flex justify-between gap-3"><dt class="text-surface-500">Status</dt><dd><Tag :value="detailUser.is_active ? 'Active' : 'Disabled'" :severity="detailUser.is_active ? 'success' : 'danger'" /></dd></div>
            <div class="flex justify-between gap-3"><dt class="text-surface-500">Email verified</dt><dd>{{ detailUser.email_verified ? 'Yes' : 'No' }}</dd></div>
          </dl>
        </section>

        <section>
          <h3 class="text-xs font-semibold uppercase text-surface-400 mb-2">Upload restrictions</h3>
          <dl class="flex flex-col gap-2">
            <div class="flex justify-between gap-3"><dt class="text-surface-500">Max file size</dt><dd>{{ formatBytes(detailUser.max_file_size_bytes) }}</dd></div>
            <div>
              <dt class="text-surface-500 mb-1">Allowed file types</dt>
              <dd>
                <div v-if="detailUser.allowed_file_types?.length" class="flex flex-wrap gap-1">
                  <Chip v-for="t in detailUser.allowed_file_types" :key="t" :label="t" />
                </div>
                <span v-else class="text-surface-400">All types</span>
              </dd>
            </div>
          </dl>
        </section>

        <section>
          <h3 class="text-xs font-semibold uppercase text-surface-400 mb-2">Activity</h3>
          <dl class="flex flex-col gap-2">
            <div class="flex justify-between gap-3"><dt class="text-surface-500">Last login</dt><dd>{{ fmtDateTime(detailUser.last_login_at) }}</dd></div>
            <div class="flex justify-between gap-3"><dt class="text-surface-500">Joined</dt><dd>{{ fmtDateTime(detailUser.created_at) }}</dd></div>
          </dl>
        </section>

        <!-- Related content (groups, API tokens, quota usage) coming in a follow-up. -->
        <div class="pt-3 border-t border-surface-200">
          <Button label="Edit user" icon="pi pi-pencil" size="small" outlined @click="editFromDetail" />
        </div>
      </div>
    </Drawer>
  </div>
</template>
