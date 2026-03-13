<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '../../api/client'

const uploads = ref<any[]>([])
const loading = ref(true)
const page = ref(1)
const total = ref(0)
const limit = 20

const tagKey = ref('')
const tagValue = ref('')
const searchQuery = ref('')

async function fetchUploads() {
  loading.value = true
  try {
    const res = await api.get('/api/v2/uploads', {
      params: { limit, offset: (page.value - 1) * limit },
    })
    uploads.value = res.data.uploads || []
    total.value = res.data.total || 0
  } catch {
    // ignore
  } finally {
    loading.value = false
  }
}

async function searchByTags() {
  loading.value = true
  try {
    const params: any = {}
    if (tagKey.value && tagValue.value) {
      params[`tag.${tagKey.value}`] = tagValue.value
    }
    if (searchQuery.value) {
      params.q = searchQuery.value
    }
    const res = await api.get('/api/v2/tags/search', { params })
    uploads.value = res.data.uploads || []
    total.value = uploads.value.length
  } catch {
    // ignore
  } finally {
    loading.value = false
  }
}

async function download(uuid: string, name: string) {
  try {
    const res = await api.get(`/api/v2/uploads/${uuid}/download`, {
      responseType: 'blob',
    })
    const url = window.URL.createObjectURL(new Blob([res.data]))
    const a = document.createElement('a')
    a.href = url
    a.download = name
    a.click()
    window.URL.revokeObjectURL(url)
  } catch {
    alert('Download failed — file may not be completed yet.')
  }
}

function formatSize(bytes: number) {
  if (!bytes) return '-'
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB'
  if (bytes < 1073741824) return (bytes / 1048576).toFixed(1) + ' MB'
  return (bytes / 1073741824).toFixed(2) + ' GB'
}

function statusClass(status: string) {
  switch (status) {
    case 'completed': return 'text-green-700 bg-green-50'
    case 'failed': return 'text-red-700 bg-red-50'
    case 'processing': return 'text-blue-700 bg-blue-50'
    default: return 'text-yellow-700 bg-yellow-50'
  }
}

onMounted(fetchUploads)
</script>

<template>
  <div class="p-6">
    <h1 class="text-2xl font-bold mb-6">Uploads</h1>

    <!-- Search / filter bar -->
    <div class="bg-white rounded-lg border border-gray-200 p-4 mb-4 flex flex-wrap gap-3 items-end">
      <div>
        <label class="block text-xs text-gray-500 mb-1">Filename</label>
        <input v-model="searchQuery" type="text" placeholder="Search..."
          class="rounded border border-gray-300 px-3 py-1.5 text-sm w-48" />
      </div>
      <div>
        <label class="block text-xs text-gray-500 mb-1">Tag Key</label>
        <input v-model="tagKey" type="text" placeholder="e.g. project"
          class="rounded border border-gray-300 px-3 py-1.5 text-sm w-32" />
      </div>
      <div>
        <label class="block text-xs text-gray-500 mb-1">Tag Value</label>
        <input v-model="tagValue" type="text" placeholder="e.g. alpha"
          class="rounded border border-gray-300 px-3 py-1.5 text-sm w-32" />
      </div>
      <button @click="searchByTags"
        class="rounded bg-gray-100 px-4 py-1.5 text-sm text-gray-700 hover:bg-gray-200">
        <i class="pi pi-search mr-1"></i> Search
      </button>
      <button @click="tagKey = ''; tagValue = ''; searchQuery = ''; fetchUploads()"
        class="rounded px-4 py-1.5 text-sm text-gray-500 hover:text-gray-700">
        Clear
      </button>
    </div>

    <!-- Table -->
    <div class="bg-white rounded-lg border border-gray-200">
      <div v-if="loading" class="p-6 text-center text-gray-400">Loading...</div>
      <div v-else-if="uploads.length === 0" class="p-6 text-center text-gray-400">No uploads found.</div>
      <table v-else class="w-full">
        <thead class="text-left text-xs text-gray-500 uppercase bg-gray-50">
          <tr>
            <th class="px-6 py-3">Name</th>
            <th class="px-6 py-3">Size</th>
            <th class="px-6 py-3">Visibility</th>
            <th class="px-6 py-3">Status</th>
            <th class="px-6 py-3">Created</th>
            <th class="px-6 py-3">Actions</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100">
          <tr v-for="u in uploads" :key="u.uuid">
            <td class="px-6 py-3 text-sm font-medium text-gray-800">{{ u.original_name }}</td>
            <td class="px-6 py-3 text-sm text-gray-500">{{ formatSize(u.file_size) }}</td>
            <td class="px-6 py-3 text-sm text-gray-500">{{ u.visibility }}</td>
            <td class="px-6 py-3">
              <span class="text-xs font-medium px-2 py-1 rounded" :class="statusClass(u.status)">
                {{ u.status }}
              </span>
            </td>
            <td class="px-6 py-3 text-sm text-gray-400">{{ new Date(u.created_at).toLocaleDateString() }}</td>
            <td class="px-6 py-3">
              <button v-if="u.status === 'completed'" @click="download(u.uuid, u.original_name)"
                class="text-blue-600 hover:text-blue-800 text-sm">
                <i class="pi pi-download mr-1"></i>Download
              </button>
            </td>
          </tr>
        </tbody>
      </table>

      <!-- Pagination -->
      <div v-if="total > limit" class="flex items-center justify-between px-6 py-3 border-t border-gray-100">
        <p class="text-sm text-gray-500">{{ total }} total</p>
        <div class="flex gap-2">
          <button @click="page--; fetchUploads()" :disabled="page <= 1"
            class="rounded border px-3 py-1 text-sm disabled:opacity-50">Prev</button>
          <button @click="page++; fetchUploads()" :disabled="page * limit >= total"
            class="rounded border px-3 py-1 text-sm disabled:opacity-50">Next</button>
        </div>
      </div>
    </div>
  </div>
</template>
