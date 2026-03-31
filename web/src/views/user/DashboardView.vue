<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../../api/client'
import { useAuthStore } from '../../stores/auth'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import Card from 'primevue/card'
import Message from 'primevue/message'

const auth = useAuthStore()
const router = useRouter()
const noWallet = ref(false)

async function checkWalletStatus() {
  try {
    const res = await api.get('/api/v2/system/wallet-status')
    noWallet.value = !res.data.has_default_wallet
  } catch {
    // ignore — non-admin may not need this
  }
}

const file = ref<File | null>(null)
const visibility = ref('private')
const uploading = ref(false)
const uploadMsg = ref('')
const uploadError = ref('')
const recentUploads = ref<any[]>([])
const loading = ref(true)

const visibilityOptions = [
  { label: 'Private', value: 'private' },
  { label: 'Public', value: 'public' },
]

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

function statusSeverity(status: string, detail?: string): string {
  if (detail === 'gas_backoff') return 'warn'
  switch (status) {
    case 'completed': return 'success'
    case 'failed': return 'danger'
    case 'processing': return 'warn'
    default: return 'info'
  }
}

function statusLabel(u: any) {
  if (u.status_detail === 'gas_backoff') {
    return `waiting (gas high)`
  }
  return u.status
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

function formatSize(bytes: number) {
  if (!bytes) return '-'
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB'
  if (bytes < 1073741824) return (bytes / 1048576).toFixed(1) + ' MB'
  return (bytes / 1073741824).toFixed(2) + ' GB'
}

onMounted(() => {
  fetchRecent()
  checkWalletStatus()
})
</script>

<template>
  <div class="p-6">
    <h1 class="text-2xl font-bold mb-1">Dashboard</h1>
    <p class="text-gray-500 mb-6">Welcome back, {{ auth.user?.first_name }}.</p>

    <!-- No wallet warning -->
    <Message v-if="noWallet" severity="warn" :closable="false" class="mb-6">
      <div class="flex items-center justify-between w-full">
        <div>
          <p class="font-medium">No wallet configured</p>
          <p class="text-sm">A wallet must be added before files can be uploaded to the network.</p>
        </div>
        <Button v-if="auth.isAdmin" label="Add Wallet" severity="warn" size="small"
          @click="router.push('/admin/wallets?add=1')" class="ml-4" />
        <span v-else class="text-xs ml-4 whitespace-nowrap">Contact your administrator</span>
      </div>
    </Message>

    <!-- Upload card -->
    <Card class="mb-6" :class="{ 'opacity-50 pointer-events-none select-none': noWallet }">
      <template #title>Upload File</template>
      <template #content>
        <Message v-if="uploadMsg" severity="success" :closable="false" class="mb-4">{{ uploadMsg }}</Message>
        <Message v-if="uploadError" severity="error" :closable="false" class="mb-4">{{ uploadError }}</Message>

        <form @submit.prevent="handleUpload" class="flex flex-col sm:flex-row items-start gap-4">
          <div class="flex-1">
            <input id="file-input" type="file" @change="onFileSelect" :disabled="noWallet"
              class="block w-full text-sm text-gray-500 file:mr-4 file:py-2 file:px-4 file:rounded file:border-0 file:text-sm file:font-medium file:bg-blue-50 file:text-blue-700 hover:file:bg-blue-100" />
          </div>
          <Select v-model="visibility" :options="visibilityOptions" optionLabel="label" optionValue="value"
            :disabled="noWallet" class="w-36" />
          <Button type="submit" :label="uploading ? 'Uploading...' : 'Upload'" icon="pi pi-upload"
            :disabled="!file || uploading || noWallet" :loading="uploading" />
        </form>
      </template>
    </Card>

    <!-- Recent uploads -->
    <Card>
      <template #title>Recent Uploads</template>
      <template #content>
        <DataTable :value="recentUploads" :loading="loading" stripedRows
          :pt="{ root: { class: '-mt-2' } }">
          <template #empty>No uploads yet. Upload your first file above.</template>
          <Column field="original_filename" header="Name" sortable />
          <Column field="file_size" header="Size" sortable>
            <template #body="{ data }">{{ formatSize(data.file_size) }}</template>
          </Column>
          <Column field="visibility" header="Visibility" sortable />
          <Column field="status" header="Status" sortable>
            <template #body="{ data }">
              <Tag :value="statusLabel(data)" :severity="statusSeverity(data.status, data.status_detail)" />
            </template>
          </Column>
          <Column field="created_at" header="Created" sortable>
            <template #body="{ data }">
              <span class="text-gray-400">{{ formatDate(data.created_at) }}</span>
            </template>
          </Column>
        </DataTable>
      </template>
    </Card>
  </div>
</template>
