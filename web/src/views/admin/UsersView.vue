<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '../../api/client'

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

function permissionBadge(perms: string) {
  if (perms?.includes('admin')) return 'text-purple-700 bg-purple-50'
  if (perms?.includes('write')) return 'text-blue-700 bg-blue-50'
  return 'text-gray-700 bg-gray-50'
}

onMounted(fetchUsers)
</script>

<template>
  <div class="p-6">
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold">User Management</h1>
      <button @click="showCreate = !showCreate"
        class="rounded bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700">
        <i class="pi pi-plus mr-1"></i> Add User
      </button>
    </div>

    <!-- Create user form -->
    <div v-if="showCreate" class="bg-white rounded-lg border border-gray-200 mb-6">
      <div class="px-6 py-4 border-b border-gray-200">
        <h2 class="text-base font-semibold text-gray-800">Create User</h2>
      </div>
      <form @submit.prevent="createUser">
        <div class="divide-y divide-gray-100">
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium text-gray-700">First Name</label>
            </div>
            <div class="col-span-2">
              <input v-model="newFirstName" type="text" required
                class="block w-full max-w-md rounded border border-gray-300 px-3 py-2 text-sm" />
            </div>
          </div>
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium text-gray-700">Last Name</label>
            </div>
            <div class="col-span-2">
              <input v-model="newLastName" type="text" required
                class="block w-full max-w-md rounded border border-gray-300 px-3 py-2 text-sm" />
            </div>
          </div>
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium text-gray-700">Email</label>
            </div>
            <div class="col-span-2">
              <input v-model="newEmail" type="email" required
                class="block w-full max-w-md rounded border border-gray-300 px-3 py-2 text-sm" />
            </div>
          </div>
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium text-gray-700">Password</label>
              <p class="text-xs text-gray-400 mt-1">Minimum 8 characters.</p>
            </div>
            <div class="col-span-2">
              <input v-model="newPassword" type="password" required minlength="8"
                class="block w-full max-w-md rounded border border-gray-300 px-3 py-2 text-sm" />
            </div>
          </div>
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium text-gray-700">Permissions</label>
              <p class="text-xs text-gray-400 mt-1">Access level for this user.</p>
            </div>
            <div class="col-span-2">
              <select v-model="newPermissions" class="block w-48 rounded border border-gray-300 px-3 py-2 text-sm">
                <option value="read">Read</option>
                <option value="read,write">Read + Write</option>
                <option value="admin,read,write">Admin</option>
              </select>
            </div>
          </div>
        </div>
        <div class="px-6 py-4 bg-gray-50 border-t border-gray-200 rounded-b-lg flex justify-end gap-2">
          <button type="button" @click="showCreate = false"
            class="rounded border px-3 py-1.5 text-sm text-gray-600 hover:bg-gray-100">Cancel</button>
          <button type="submit" :disabled="creating"
            class="rounded bg-blue-600 px-4 py-1.5 text-sm text-white hover:bg-blue-700 disabled:opacity-50">
            {{ creating ? 'Creating...' : 'Create User' }}
          </button>
        </div>
      </form>
    </div>

    <!-- Edit permissions modal -->
    <div v-if="editingUser" class="fixed inset-0 bg-black/30 flex items-center justify-center z-50">
      <div class="bg-white rounded-lg shadow-xl w-full max-w-lg">
        <div class="px-6 py-4 border-b border-gray-200">
          <h2 class="text-base font-semibold text-gray-800">
            Edit Permissions: {{ editingUser.first_name }} {{ editingUser.last_name }}
          </h2>
        </div>
        <div class="grid grid-cols-3 gap-6 px-6 py-5">
          <div>
            <label class="text-sm font-medium text-gray-700">Permissions</label>
            <p class="text-xs text-gray-400 mt-1">Access level for this user.</p>
          </div>
          <div class="col-span-2">
            <select v-model="editPermissions" class="block w-48 rounded border border-gray-300 px-3 py-2 text-sm">
              <option value="read">Read</option>
              <option value="read,write">Read + Write</option>
              <option value="admin,read,write">Admin</option>
            </select>
          </div>
        </div>
        <div class="px-6 py-4 bg-gray-50 border-t border-gray-200 rounded-b-lg flex justify-end gap-2">
          <button @click="editingUser = null"
            class="rounded border px-3 py-1.5 text-sm text-gray-600 hover:bg-gray-100">Cancel</button>
          <button @click="savePermissions" :disabled="saving"
            class="rounded bg-blue-600 px-4 py-1.5 text-sm text-white hover:bg-blue-700 disabled:opacity-50">
            {{ saving ? 'Saving...' : 'Save' }}
          </button>
        </div>
      </div>
    </div>

    <!-- Users table -->
    <div class="bg-white rounded-lg border border-gray-200">
      <div v-if="loading" class="p-6 text-center text-gray-400">Loading...</div>
      <div v-else-if="users.length === 0" class="p-6 text-center text-gray-400">No users found.</div>
      <table v-else class="w-full">
        <thead class="text-left text-xs text-gray-500 uppercase bg-gray-50">
          <tr>
            <th class="px-6 py-3">Name</th>
            <th class="px-6 py-3">Email</th>
            <th class="px-6 py-3">Permissions</th>
            <th class="px-6 py-3">Status</th>
            <th class="px-6 py-3">Joined</th>
            <th class="px-6 py-3">Actions</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100">
          <tr v-for="u in users" :key="u.id">
            <td class="px-6 py-3 text-sm font-medium text-gray-800">
              {{ u.first_name }} {{ u.last_name }}
              <span v-if="u.is_service_account" class="ml-1 text-xs text-gray-400">(service)</span>
            </td>
            <td class="px-6 py-3 text-sm text-gray-500">{{ u.email }}</td>
            <td class="px-6 py-3">
              <span class="text-xs font-medium px-2 py-1 rounded" :class="permissionBadge(u.permissions)">
                {{ u.permissions || 'read' }}
              </span>
            </td>
            <td class="px-6 py-3 text-sm">
              <span :class="u.is_active ? 'text-green-600' : 'text-red-500'">
                {{ u.is_active ? 'Active' : 'Disabled' }}
              </span>
            </td>
            <td class="px-6 py-3 text-sm text-gray-400">{{ formatDate(u.created_at) }}</td>
            <td class="px-6 py-3 flex gap-2">
              <button @click="startEdit(u)" class="text-blue-600 hover:text-blue-800 text-sm">
                <i class="pi pi-pencil"></i>
              </button>
              <button @click="deleteUser(u.id)" class="text-red-600 hover:text-red-800 text-sm">
                <i class="pi pi-trash"></i>
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
