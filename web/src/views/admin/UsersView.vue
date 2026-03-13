<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '../../api/client'

const users = ref<any[]>([])
const loading = ref(true)
const editingUser = ref<any>(null)
const editPermissions = ref('')
const saving = ref(false)

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

function permissionBadge(perms: string) {
  if (perms?.includes('admin')) return 'text-purple-700 bg-purple-50'
  if (perms?.includes('write')) return 'text-blue-700 bg-blue-50'
  return 'text-gray-700 bg-gray-50'
}

onMounted(fetchUsers)
</script>

<template>
  <div class="p-6">
    <h1 class="text-2xl font-bold mb-6">User Management</h1>

    <!-- Edit permissions modal -->
    <div v-if="editingUser" class="fixed inset-0 bg-black/30 flex items-center justify-center z-50">
      <div class="bg-white rounded-lg shadow-xl p-6 w-full max-w-md">
        <h2 class="text-lg font-semibold mb-4">
          Edit Permissions: {{ editingUser.first_name }} {{ editingUser.last_name }}
        </h2>
        <div class="mb-4">
          <label class="block text-sm font-medium text-gray-700 mb-1">Permissions</label>
          <select v-model="editPermissions" class="block w-full rounded border border-gray-300 px-3 py-2 text-sm">
            <option value="read">Read</option>
            <option value="read,write">Read + Write</option>
            <option value="admin,read,write">Admin</option>
          </select>
        </div>
        <div class="flex gap-3 justify-end">
          <button @click="editingUser = null"
            class="rounded border px-4 py-2 text-sm text-gray-600 hover:bg-gray-50">Cancel</button>
          <button @click="savePermissions" :disabled="saving"
            class="rounded bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700 disabled:opacity-50">
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
            <td class="px-6 py-3 text-sm text-gray-400">{{ new Date(u.created_at).toLocaleDateString() }}</td>
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
