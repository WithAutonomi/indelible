<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../../api/client'
import { useAuthStore } from '../../stores/auth'

const auth = useAuthStore()
const router = useRouter()
const noWallet = ref(false)

async function checkWalletStatus() {
  try {
    const res = await api.get('/api/v2/system/wallet-status')
    noWallet.value = !res.data.has_default_wallet
  } catch {
    // ignore
  }
}

const uploads = ref<any[]>([])
const loading = ref(true)
const page = ref(1)
const total = ref(0)
const limit = 20

const tagKey = ref('')
const tagValue = ref('')
const searchQuery = ref('')

const file = ref<File | null>(null)
const visibility = ref('private')
const uploading = ref(false)
const uploadMsg = ref('')
const uploadError = ref('')

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
    const input = document.getElementById('upload-file-input') as HTMLInputElement
    if (input) input.value = ''
    await fetchUploads()
  } catch (e: any) {
    uploadError.value = e.response?.data?.error || 'Upload failed'
  } finally {
    uploading.value = false
  }
}

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

async function cancelUpload(uuid: string) {
  if (!confirm('Cancel this upload?')) return
  try {
    await api.post(`/api/v2/uploads/${uuid}/cancel`)
    await fetchUploads()
  } catch (e: any) {
    alert(e.response?.data?.error || 'Failed to cancel upload')
  }
}

async function retryUpload(uuid: string) {
  try {
    await api.post(`/api/v2/uploads/${uuid}/retry`)
    await fetchUploads()
  } catch (e: any) {
    alert(e.response?.data?.error || 'Failed to retry upload')
  }
}

async function forceRetry(uuid: string) {
  try {
    await api.post(`/api/v2/uploads/${uuid}/force-retry`)
    await fetchUploads()
  } catch (e: any) {
    alert(e.response?.data?.error || 'Failed to force retry')
  }
}

async function deleteUpload(uuid: string) {
  if (!confirm('Permanently delete this upload record?')) return
  try {
    await api.delete(`/api/v2/uploads/${uuid}`)
    await fetchUploads()
  } catch (e: any) {
    alert(e.response?.data?.error || 'Failed to delete upload')
  }
}

function formatSize(bytes: number) {
  if (!bytes) return '-'
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB'
  if (bytes < 1073741824) return (bytes / 1048576).toFixed(1) + ' MB'
  return (bytes / 1073741824).toFixed(2) + ' GB'
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

function statusClass(status: string, detail?: string) {
  if (detail === 'gas_backoff') return 'text-orange-700 bg-orange-50'
  switch (status) {
    case 'completed': return 'text-green-700 bg-green-50'
    case 'failed': return 'text-red-700 bg-red-50'
    case 'processing': return 'text-blue-700 bg-blue-50'
    default: return 'text-yellow-700 bg-yellow-50'
  }
}

function statusLabel(u: any) {
  if (u.status_detail === 'gas_backoff') {
    return `waiting (gas high, attempt ${u.backoff_attempt})`
  }
  return u.status
}

onMounted(() => {
  fetchUploads()
  checkWalletStatus()
})
</script>

<template>
  <div class="p-6">
    <h1 class="text-2xl font-bold mb-6">Uploads</h1>

    <!-- No wallet warning -->
    <div v-if="noWallet" class="mb-4 rounded-lg border border-amber-300 bg-amber-50 p-4 flex items-center justify-between">
      <div>
        <p class="text-sm font-medium text-amber-800">No wallet configured</p>
        <p class="text-sm text-amber-700">A wallet must be added before files can be uploaded to the network.</p>
      </div>
      <button v-if="auth.isAdmin" @click="router.push('/admin/wallets?add=1')"
        class="rounded bg-amber-600 px-4 py-2 text-sm text-white hover:bg-amber-700 whitespace-nowrap ml-4">
        Add Wallet
      </button>
      <span v-else class="text-xs text-amber-600 ml-4 whitespace-nowrap">Contact your administrator</span>
    </div>

    <!-- Upload card -->
    <div class="bg-white rounded-lg border border-gray-200 p-4 mb-4" :class="{ 'opacity-50 pointer-events-none select-none': noWallet }">
      <div v-if="uploadMsg" class="mb-3 rounded bg-green-50 p-3 text-green-700 text-sm">{{ uploadMsg }}</div>
      <div v-if="uploadError" class="mb-3 rounded bg-red-50 p-3 text-red-700 text-sm">{{ uploadError }}</div>
      <form @submit.prevent="handleUpload" class="flex flex-col sm:flex-row items-start gap-4">
        <div class="flex-1">
          <input id="upload-file-input" type="file" @change="onFileSelect" :disabled="noWallet"
            class="block w-full text-sm text-gray-500 file:mr-4 file:py-2 file:px-4 file:rounded file:border-0 file:text-sm file:font-medium file:bg-blue-50 file:text-blue-700 hover:file:bg-blue-100" />
        </div>
        <select v-model="visibility" :disabled="noWallet" class="rounded border border-gray-300 px-3 py-2 text-sm">
          <option value="private">Private</option>
          <option value="public">Public</option>
        </select>
        <button type="submit" :disabled="!file || uploading || noWallet"
          class="rounded bg-blue-600 px-5 py-2 text-sm text-white hover:bg-blue-700 disabled:opacity-50">
          {{ uploading ? 'Uploading...' : 'Upload' }}
        </button>
      </form>
    </div>

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
            <td class="px-6 py-3 text-sm font-medium text-gray-800">{{ u.original_filename }}</td>
            <td class="px-6 py-3 text-sm text-gray-500">{{ formatSize(u.file_size) }}</td>
            <td class="px-6 py-3 text-sm text-gray-500">{{ u.visibility }}</td>
            <td class="px-6 py-3">
              <span class="text-xs font-medium px-2 py-1 rounded" :class="statusClass(u.status, u.status_detail)">
                {{ statusLabel(u) }}
              </span>
              <p v-if="u.error_message" class="text-xs text-red-500 mt-1" :title="u.error_message">
                {{ u.error_message.length > 50 ? u.error_message.substring(0, 50) + '...' : u.error_message }}
              </p>
            </td>
            <td class="px-6 py-3 text-sm text-gray-400">{{ formatDate(u.created_at) }}</td>
            <td class="px-6 py-3">
              <div class="flex gap-2 items-center">
                <!-- Completed: download, delete -->
                <button v-if="u.status === 'completed'" @click="download(u.uuid, u.original_filename)"
                  class="text-blue-600 hover:text-blue-800 text-sm" title="Download">
                  <i class="pi pi-download"></i>
                </button>
                <button v-if="u.status === 'completed'" @click="deleteUpload(u.uuid)"
                  class="text-red-500 hover:text-red-700 text-sm" title="Delete">
                  <i class="pi pi-trash"></i>
                </button>
                <!-- Queued (not backoff): cancel -->
                <button v-if="u.status === 'queued' && u.status_detail !== 'gas_backoff'" @click="cancelUpload(u.uuid)"
                  class="text-red-500 hover:text-red-700 text-sm" title="Cancel">
                  <i class="pi pi-times"></i>
                </button>
                <!-- Processing: cancel -->
                <button v-if="u.status === 'processing'" @click="cancelUpload(u.uuid)"
                  class="text-red-500 hover:text-red-700 text-sm" title="Cancel">
                  <i class="pi pi-times"></i>
                </button>
                <!-- Gas backoff: force retry, cancel -->
                <button v-if="u.status_detail === 'gas_backoff'" @click="forceRetry(u.uuid)"
                  class="text-blue-600 hover:text-blue-800 text-sm" title="Retry now">
                  <i class="pi pi-refresh"></i>
                </button>
                <button v-if="u.status_detail === 'gas_backoff'" @click="cancelUpload(u.uuid)"
                  class="text-red-500 hover:text-red-700 text-sm" title="Cancel">
                  <i class="pi pi-times"></i>
                </button>
                <!-- Failed: retry, delete -->
                <button v-if="u.status === 'failed'" @click="retryUpload(u.uuid)"
                  class="text-blue-600 hover:text-blue-800 text-sm" title="Retry">
                  <i class="pi pi-refresh"></i>
                </button>
                <button v-if="u.status === 'failed'" @click="deleteUpload(u.uuid)"
                  class="text-red-500 hover:text-red-700 text-sm" title="Delete">
                  <i class="pi pi-trash"></i>
                </button>
              </div>
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
