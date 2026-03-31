<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useConfirm } from 'primevue/useconfirm'
import { api } from '../../api/client'
import { useAuthStore } from '../../stores/auth'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import Card from 'primevue/card'
import Dialog from 'primevue/dialog'
import Message from 'primevue/message'
import ConfirmDialog from 'primevue/confirmdialog'

const auth = useAuthStore()
const router = useRouter()
const confirm = useConfirm()
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

const visibilityOptions = [
  { label: 'Private', value: 'private' },
  { label: 'Public', value: 'public' },
]

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

const downloading = ref<string | null>(null)

async function download(uuid: string, name: string) {
  downloading.value = uuid
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
  } finally {
    downloading.value = null
  }
}

function cancelUpload(uuid: string) {
  confirm.require({
    message: 'Cancel this upload?',
    header: 'Confirm Cancel',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: async () => {
      try {
        await api.post(`/api/v2/uploads/${uuid}/cancel`)
        await fetchUploads()
      } catch (e: any) {
        alert(e.response?.data?.error || 'Failed to cancel upload')
      }
    },
  })
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

function deleteUpload(uuid: string) {
  confirm.require({
    message: 'Permanently delete this upload record?',
    header: 'Confirm Delete',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: async () => {
      try {
        await api.delete(`/api/v2/uploads/${uuid}`)
        await fetchUploads()
      } catch (e: any) {
        alert(e.response?.data?.error || 'Failed to delete upload')
      }
    },
  })
}

// Tags management
const tagsDialogVisible = ref(false)
const tagsUploadUuid = ref('')
const tagsUploadName = ref('')
const tags = ref<{ key: string; value: string }[]>([])
const newTagKey = ref('')
const newTagValue = ref('')
const tagsSaving = ref(false)

async function openTags(uuid: string, name: string) {
  tagsUploadUuid.value = uuid
  tagsUploadName.value = name
  tagsDialogVisible.value = true
  try {
    const res = await api.get(`/api/v2/uploads/${uuid}/tags`)
    const tagMap = res.data.tags || {}
    tags.value = Object.entries(tagMap).map(([key, value]) => ({ key, value: value as string }))
  } catch {
    tags.value = []
  }
}

function addTag() {
  const k = newTagKey.value.trim()
  const v = newTagValue.value.trim()
  if (!k || !v) return
  if (tags.value.some(t => t.key === k)) {
    tags.value = tags.value.map(t => t.key === k ? { key: k, value: v } : t)
  } else {
    tags.value.push({ key: k, value: v })
  }
  newTagKey.value = ''
  newTagValue.value = ''
}

function removeTag(key: string) {
  tags.value = tags.value.filter(t => t.key !== key)
}

async function saveTags() {
  tagsSaving.value = true
  try {
    const tagMap: Record<string, string> = {}
    for (const t of tags.value) tagMap[t.key] = t.value
    await api.put(`/api/v2/uploads/${tagsUploadUuid.value}/tags`, { tags: tagMap })
    tagsDialogVisible.value = false
  } catch (e: any) {
    alert(e.response?.data?.error || 'Failed to save tags')
  } finally {
    tagsSaving.value = false
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
    return `waiting (gas high, attempt ${u.backoff_attempt})`
  }
  return u.status
}

function onPage(event: any) {
  page.value = event.page + 1
  fetchUploads()
}

onMounted(() => {
  fetchUploads()
  checkWalletStatus()
})
</script>

<template>
  <div class="p-6">
    <ConfirmDialog />

    <h1 class="text-2xl font-bold mb-6">Uploads</h1>

    <!-- No wallet warning -->
    <Message v-if="noWallet" severity="warn" :closable="false" class="mb-4">
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
    <Card class="mb-4" :class="{ 'opacity-50 pointer-events-none select-none': noWallet }">
      <template #content>
        <Message v-if="uploadMsg" severity="success" :closable="false" class="mb-4">{{ uploadMsg }}</Message>
        <Message v-if="uploadError" severity="error" :closable="false" class="mb-4">{{ uploadError }}</Message>
        <form @submit.prevent="handleUpload" class="flex flex-col sm:flex-row items-start gap-4">
          <div class="flex-1">
            <input id="upload-file-input" type="file" @change="onFileSelect" :disabled="noWallet"
              class="block w-full text-sm text-gray-500 file:mr-4 file:py-2 file:px-4 file:rounded file:border-0 file:text-sm file:font-medium file:bg-blue-50 file:text-blue-700 hover:file:bg-blue-100" />
          </div>
          <Select v-model="visibility" :options="visibilityOptions" optionLabel="label" optionValue="value"
            :disabled="noWallet" class="w-36" />
          <Button type="submit" :label="uploading ? 'Uploading...' : 'Upload'" icon="pi pi-upload"
            :disabled="!file || uploading || noWallet" :loading="uploading" />
        </form>
      </template>
    </Card>

    <!-- Search / filter bar -->
    <Card class="mb-4">
      <template #content>
        <div class="flex flex-wrap gap-3 items-end">
          <div>
            <label class="block text-xs text-gray-500 mb-1">Filename</label>
            <InputText v-model="searchQuery" placeholder="Search..." size="small" class="w-48" />
          </div>
          <div>
            <label class="block text-xs text-gray-500 mb-1">Tag Key</label>
            <InputText v-model="tagKey" placeholder="e.g. project" size="small" class="w-32" />
          </div>
          <div>
            <label class="block text-xs text-gray-500 mb-1">Tag Value</label>
            <InputText v-model="tagValue" placeholder="e.g. alpha" size="small" class="w-32" />
          </div>
          <Button label="Search" icon="pi pi-search" severity="secondary" size="small" @click="searchByTags" />
          <Button label="Clear" text size="small"
            @click="tagKey = ''; tagValue = ''; searchQuery = ''; fetchUploads()" />
        </div>
      </template>
    </Card>

    <!-- Table -->
    <Card>
      <template #content>
        <DataTable :value="uploads" :loading="loading" stripedRows
          paginator :rows="limit" :totalRecords="total" :lazy="true" @page="onPage"
          :pt="{ root: { class: '-mt-2' } }">
          <template #empty>No uploads found.</template>
          <Column field="original_filename" header="Name" sortable>
            <template #body="{ data }">
              <span :class="data.visibility === 'public' ? 'text-amber-600 font-medium' : ''">
                {{ data.original_filename }}
              </span>
              <i v-if="data.visibility === 'public'" class="pi pi-info-circle text-amber-500 ml-1.5 text-xs"
                v-tooltip.top="'This file is publicly accessible on the network'" />
            </template>
          </Column>
          <Column field="file_size" header="Size" sortable>
            <template #body="{ data }">{{ formatSize(data.file_size) }}</template>
          </Column>
          <Column field="status" header="Status" sortable>
            <template #body="{ data }">
              <div>
                <Tag :value="statusLabel(data)" :severity="statusSeverity(data.status, data.status_detail)" />
                <p v-if="data.error_message" class="text-xs text-red-500 mt-1" :title="data.error_message">
                  {{ data.error_message.length > 50 ? data.error_message.substring(0, 50) + '...' : data.error_message }}
                </p>
              </div>
            </template>
          </Column>
          <Column field="created_at" header="Created" sortable>
            <template #body="{ data }">
              <span class="text-gray-400">{{ formatDate(data.created_at) }}</span>
            </template>
          </Column>
          <Column header="Actions">
            <template #body="{ data }">
              <div class="flex gap-1 items-center">
                <!-- Completed: download, delete -->
                <Button v-if="data.status === 'completed'" icon="pi pi-download" label="Download"
                  outlined size="small"
                  :loading="downloading === data.uuid" @click="download(data.uuid, data.original_filename)" />
                <Button v-if="data.status === 'completed'" icon="pi pi-tag" text rounded size="small"
                  severity="secondary" @click="openTags(data.uuid, data.original_filename)" v-tooltip.top="'Tags'" />
                <Button v-if="data.status === 'completed'" icon="pi pi-trash" text rounded size="small"
                  severity="secondary" @click="deleteUpload(data.uuid)" v-tooltip.top="'Delete'" />

                <!-- Queued (not backoff): cancel -->
                <Button v-if="data.status === 'queued' && data.status_detail !== 'gas_backoff'"
                  icon="pi pi-times" text rounded size="small" severity="danger"
                  @click="cancelUpload(data.uuid)" v-tooltip.top="'Cancel'" />

                <!-- Processing: cancel -->
                <Button v-if="data.status === 'processing'" icon="pi pi-times" text rounded size="small"
                  severity="danger" @click="cancelUpload(data.uuid)" v-tooltip.top="'Cancel'" />

                <!-- Gas backoff: force retry, cancel -->
                <Button v-if="data.status_detail === 'gas_backoff'" icon="pi pi-refresh" text rounded size="small"
                  @click="forceRetry(data.uuid)" v-tooltip.top="'Retry now'" />
                <Button v-if="data.status_detail === 'gas_backoff'" icon="pi pi-times" text rounded size="small"
                  severity="danger" @click="cancelUpload(data.uuid)" v-tooltip.top="'Cancel'" />

                <!-- Failed: retry, delete -->
                <Button v-if="data.status === 'failed'" icon="pi pi-refresh" text rounded size="small"
                  @click="retryUpload(data.uuid)" v-tooltip.top="'Retry'" />
                <Button v-if="data.status === 'failed'" icon="pi pi-trash" text rounded size="small"
                  severity="danger" @click="deleteUpload(data.uuid)" v-tooltip.top="'Delete'" />
              </div>
            </template>
          </Column>
        </DataTable>
      </template>
    </Card>

    <!-- Tags dialog -->
    <Dialog v-model:visible="tagsDialogVisible" :header="'Tags: ' + tagsUploadName" modal class="w-full max-w-lg">
      <div class="space-y-4">
        <!-- Existing tags -->
        <div v-if="tags.length" class="space-y-2">
          <div v-for="t in tags" :key="t.key" class="flex items-center gap-2">
            <Tag :value="t.key" severity="info" />
            <span class="text-sm">=</span>
            <span class="text-sm">{{ t.value }}</span>
            <Button icon="pi pi-times" text rounded size="small" severity="danger" @click="removeTag(t.key)" />
          </div>
        </div>
        <p v-else class="text-sm text-surface-400">No tags yet.</p>

        <!-- Add tag -->
        <div class="flex gap-2 items-end pt-2 border-t border-surface-200">
          <div class="flex-1">
            <label class="block text-xs text-surface-500 mb-1">Key</label>
            <InputText v-model="newTagKey" placeholder="e.g. department" size="small" class="w-full" @keyup.enter="addTag" />
          </div>
          <div class="flex-1">
            <label class="block text-xs text-surface-500 mb-1">Value</label>
            <InputText v-model="newTagValue" placeholder="e.g. engineering" size="small" class="w-full" @keyup.enter="addTag" />
          </div>
          <Button icon="pi pi-plus" size="small" outlined @click="addTag" :disabled="!newTagKey.trim() || !newTagValue.trim()" />
        </div>
      </div>

      <template #footer>
        <Button label="Cancel" text @click="tagsDialogVisible = false" />
        <Button label="Save" icon="pi pi-check" :loading="tagsSaving" @click="saveTags" />
      </template>
    </Dialog>
  </div>
</template>
