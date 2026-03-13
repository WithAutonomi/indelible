<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '../../api/client'
import { useAuthStore } from '../../stores/auth'

const auth = useAuthStore()

const file = ref<File | null>(null)
const visibility = ref('private')
const uploading = ref(false)
const uploadMsg = ref('')
const uploadError = ref('')
const recentUploads = ref<any[]>([])
const loading = ref(true)

async function fetchRecent() {
  try {
    const res = await api.get('/api/v2/uploads?limit=10')
    recentUploads.value = res.data.uploads || []
  } catch {
    // ignore
  } finally {
    loading.value = false
  }
}

function onFileSelect(e: Event) {
  const target = e.target as HTMLInputElement
  file.value = target.files?.[0] || null
}

async function handleUpload() {
  if (!file.value) return
  uploading.value = true
  uploadMsg.value = ''
  uploadError.value = ''

  const formData = new FormData()
  formData.append('file', file.value)
  formData.append('visibility', visibility.value)

  try {
    await api.post('/api/v2/uploads', formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    })
    uploadMsg.value = 'File queued for upload!'
    file.value = null
    const input = document.getElementById('file-input') as HTMLInputElement
    if (input) input.value = ''
    await fetchRecent()
  } catch (e: any) {
    uploadError.value = e.response?.data?.error || 'Upload failed'
  } finally {
    uploading.value = false
  }
}

function statusClass(status: string) {
  switch (status) {
    case 'completed': return 'text-green-700 bg-green-50'
    case 'failed': return 'text-red-700 bg-red-50'
    case 'processing': return 'text-blue-700 bg-blue-50'
    default: return 'text-yellow-700 bg-yellow-50'
  }
}

function formatSize(bytes: number) {
  if (!bytes) return '-'
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB'
  if (bytes < 1073741824) return (bytes / 1048576).toFixed(1) + ' MB'
  return (bytes / 1073741824).toFixed(2) + ' GB'
}

onMounted(fetchRecent)
</script>

<template>
  <div class="p-6 max-w-5xl">
    <h1 class="text-2xl font-bold mb-1">Dashboard</h1>
    <p class="text-gray-500 mb-6">Welcome back, {{ auth.user?.first_name }}.</p>

    <!-- Upload card -->
    <div class="bg-white rounded-lg border border-gray-200 p-6 mb-6">
      <h2 class="text-lg font-semibold mb-4">Upload File</h2>
      <div v-if="uploadMsg" class="mb-3 rounded bg-green-50 p-3 text-green-700 text-sm">{{ uploadMsg }}</div>
      <div v-if="uploadError" class="mb-3 rounded bg-red-50 p-3 text-red-700 text-sm">{{ uploadError }}</div>

      <form @submit.prevent="handleUpload" class="flex flex-col sm:flex-row items-start gap-4">
        <div class="flex-1">
          <input id="file-input" type="file" @change="onFileSelect"
            class="block w-full text-sm text-gray-500 file:mr-4 file:py-2 file:px-4 file:rounded file:border-0 file:text-sm file:font-medium file:bg-blue-50 file:text-blue-700 hover:file:bg-blue-100" />
        </div>
        <select v-model="visibility" class="rounded border border-gray-300 px-3 py-2 text-sm">
          <option value="private">Private</option>
          <option value="public">Public</option>
        </select>
        <button type="submit" :disabled="!file || uploading"
          class="rounded bg-blue-600 px-5 py-2 text-sm text-white hover:bg-blue-700 disabled:opacity-50">
          {{ uploading ? 'Uploading...' : 'Upload' }}
        </button>
      </form>
    </div>

    <!-- Recent uploads -->
    <div class="bg-white rounded-lg border border-gray-200">
      <div class="px-6 py-4 border-b border-gray-200">
        <h2 class="text-lg font-semibold">Recent Uploads</h2>
      </div>
      <div v-if="loading" class="p-6 text-center text-gray-400">Loading...</div>
      <div v-else-if="recentUploads.length === 0" class="p-6 text-center text-gray-400">
        No uploads yet. Upload your first file above.
      </div>
      <table v-else class="w-full">
        <thead class="text-left text-xs text-gray-500 uppercase bg-gray-50">
          <tr>
            <th class="px-6 py-3">Name</th>
            <th class="px-6 py-3">Size</th>
            <th class="px-6 py-3">Visibility</th>
            <th class="px-6 py-3">Status</th>
            <th class="px-6 py-3">Created</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100">
          <tr v-for="u in recentUploads" :key="u.uuid">
            <td class="px-6 py-3 text-sm font-medium text-gray-800">{{ u.original_name }}</td>
            <td class="px-6 py-3 text-sm text-gray-500">{{ formatSize(u.file_size) }}</td>
            <td class="px-6 py-3 text-sm text-gray-500">{{ u.visibility }}</td>
            <td class="px-6 py-3">
              <span class="text-xs font-medium px-2 py-1 rounded" :class="statusClass(u.status)">
                {{ u.status }}
              </span>
            </td>
            <td class="px-6 py-3 text-sm text-gray-400">{{ new Date(u.created_at).toLocaleDateString() }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
