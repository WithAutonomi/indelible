<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import { api } from '../../api/client'
import { useAuthStore } from '../../stores/auth'
import type { Upload, Collection } from '../../types/api'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import Card from 'primevue/card'
import Dialog from 'primevue/dialog'
import AutoComplete from 'primevue/autocomplete'
import Skeleton from 'primevue/skeleton'
import Message from 'primevue/message'
import Drawer from 'primevue/drawer'
import DatePicker from 'primevue/datepicker'
import { presetRange, PRESET_OPTIONS, type DatePreset } from '../../composables/useDateRangePresets'

const auth = useAuthStore()
const router = useRouter()
const confirm = useConfirm()
const toast = useToast()
const noWallet = ref(false)

async function checkWalletStatus() {
  try {
    const res = await api.get('/api/v2/system/wallet-status')
    noWallet.value = !res.data.has_default_wallet
  } catch {
    // ignore
  }
}

const uploads = ref<Upload[]>([])
const selectedUploads = ref<Upload[]>([])
const loading = ref(true)
const page = ref(1)
const total = ref(0)
const limit = 20
const bulkProcessing = ref(false)

// Server-side sort state for the lazy DataTable. sortOrder follows PrimeVue's
// convention: 1 = asc, -1 = desc.
const sortField = ref('')
const sortOrder = ref(-1)
// DataTable column `field` -> backend sort key (the /uploads whitelist accepts
// created_at | file_size | filename | status; the Name column's field is
// original_filename).
const SORT_FIELD_MAP: Record<string, string> = {
  original_filename: 'filename',
  file_size: 'file_size',
  status: 'status',
  created_at: 'created_at',
}

const tagKey = ref('')
const tagValue = ref('')
const searchQuery = ref('')
// V2-410: date-range filtering (presets shared with the Logs filter)
const datePreset = ref<DatePreset>('')
const sinceDate = ref<Date | null>(null)
const untilDate = ref<Date | null>(null)
const tagKeySuggestions = ref<string[]>([])
const tagValueSuggestions = ref<string[]>([])
let keyDebounce: ReturnType<typeof setTimeout> | null = null
let valueDebounce: ReturnType<typeof setTimeout> | null = null

function searchTagKeys(event: { query: string }) {
  if (keyDebounce) clearTimeout(keyDebounce)
  keyDebounce = setTimeout(async () => {
    try {
      const res = await api.get('/api/v2/tags/keys')
      const all: string[] = res.data.keys || []
      tagKeySuggestions.value = event.query
        ? all.filter(k => k.toLowerCase().includes(event.query.toLowerCase()))
        : all
    } catch {
      tagKeySuggestions.value = []
    }
  }, 300)
}

function searchTagValues(event: { query: string }) {
  if (valueDebounce) clearTimeout(valueDebounce)
  if (!tagKey.value) {
    tagValueSuggestions.value = []
    return
  }
  valueDebounce = setTimeout(async () => {
    try {
      const res = await api.get('/api/v2/tags/values', { params: { key: tagKey.value } })
      const all: string[] = res.data.values || []
      tagValueSuggestions.value = event.query
        ? all.filter(v => v.toLowerCase().includes(event.query.toLowerCase()))
        : all
    } catch {
      tagValueSuggestions.value = []
    }
  }, 300)
}

const file = ref<File | null>(null)
const visibility = ref('private')
const uploading = ref(false)
const uploadTagKey = ref('')
const uploadTagValue = ref('')
const uploadTags = ref<{ key: string; value: string }[]>([])

const visibilityOptions = [
  { label: 'Private', value: 'private' },
  { label: 'Public', value: 'public' },
]

function addUploadTag() {
  const k = uploadTagKey.value.trim()
  const v = uploadTagValue.value.trim()
  if (!k || !v) return
  const existing = uploadTags.value.findIndex(t => t.key === k)
  if (existing >= 0) {
    uploadTags.value[existing].value = v
  } else {
    uploadTags.value.push({ key: k, value: v })
  }
  uploadTagKey.value = ''
  uploadTagValue.value = ''
}

function removeUploadTag(key: string) {
  uploadTags.value = uploadTags.value.filter(t => t.key !== key)
}

function onFileSelect(e: Event) {
  const target = e.target as HTMLInputElement
  file.value = target.files?.[0] || null
}

async function handleUpload() {
  if (!file.value) return
  uploading.value = true

  const formData = new FormData()
  formData.append('file', file.value)
  formData.append('visibility', visibility.value)

  // Include tags if any were added
  if (uploadTags.value.length > 0) {
    const tagMap: Record<string, string> = {}
    for (const t of uploadTags.value) tagMap[t.key] = t.value
    formData.append('tags', JSON.stringify(tagMap))
  }

  try {
    await api.post('/api/v2/uploads', formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
    })
    toast.add({ severity: 'success', summary: 'Queued', detail: 'File queued for upload', life: 3000 })
    file.value = null
    uploadTags.value = []
    const input = document.getElementById('upload-file-input') as HTMLInputElement
    if (input) input.value = ''
    await fetchUploads()
  } catch (e: any) {
    toast.add({ severity: 'error', summary: 'Upload Failed', detail: e.response?.data?.error || 'Upload failed', life: 5000 })
  } finally {
    uploading.value = false
  }
}

// V2-410: build the created_at filter. An active preset sends precise RFC3339
// timestamps (exact rolling windows); manual pickers send the selected day's
// start/end. The /uploads endpoint filters created_at via from/to (RFC3339).
function applyDateParams(params: any) {
  const range = presetRange(datePreset.value)
  if (range) {
    params.from = range.since.toISOString()
    params.to = range.until.toISOString()
    return
  }
  if (sinceDate.value) {
    const s = new Date(sinceDate.value)
    s.setHours(0, 0, 0, 0)
    params.from = s.toISOString()
  }
  if (untilDate.value) {
    const u = new Date(untilDate.value)
    u.setHours(23, 59, 59, 999)
    params.to = u.toISOString()
  }
}

// Selecting a preset clears the manual pickers; editing a picker drops back to
// Custom. Both refetch the list from page 1.
function onDatePresetChange() {
  if (datePreset.value) {
    sinceDate.value = null
    untilDate.value = null
  }
  page.value = 1
  fetchUploads()
}

function onManualDate() {
  datePreset.value = ''
  page.value = 1
  fetchUploads()
}

async function fetchUploads() {
  loading.value = true
  try {
    const params: any = { limit, offset: (page.value - 1) * limit }
    if (sortField.value) {
      const backendField = SORT_FIELD_MAP[sortField.value] || sortField.value
      params.sort = `${backendField}:${sortOrder.value === 1 ? 'asc' : 'desc'}`
    }
    applyDateParams(params)
    const res = await api.get('/api/v2/uploads', { params })
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
    // /tags/search returns { results: [{ upload, tags }], total } — not the
    // { uploads, total } shape that /uploads uses. Reading the wrong key here
    // made every search silently clear the table.
    const results = res.data.results || []
    uploads.value = results.map((r: any) => r.upload)
    total.value = res.data.total ?? uploads.value.length
  } catch {
    // ignore
  } finally {
    loading.value = false
  }
}

// Unified search entry point. Empty criteria restores the full paginated list;
// otherwise hit the search endpoint. Wired to the button, Enter, and a
// debounced watch so the bar responds however the user drives it.
function runSearch() {
  if (!searchQuery.value.trim() && !(tagKey.value && tagValue.value)) {
    page.value = 1
    fetchUploads()
  } else {
    searchByTags()
  }
}

let searchTimer: ReturnType<typeof setTimeout> | null = null
watch(searchQuery, () => {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(runSearch, 300)
})

// --- Per-file detail panel (V2-403): right-side Drawer opened on row click.
// Core fields come straight from the clicked row; tags + collection membership
// are fetched lazily. ---
const detailVisible = ref(false)
const detail = ref<Upload | null>(null)
const detailTags = ref<{ key: string; value: string }[]>([])
const detailCollections = ref<string[]>([])
const detailLoading = ref(false)
let collectionNameCache: Record<number, string> | null = null

async function openDetail(row: Upload) {
  detail.value = row
  detailVisible.value = true
  detailTags.value = []
  detailCollections.value = []
  detailLoading.value = true
  try {
    const [tagsRes, collRes] = await Promise.all([
      api.get(`/api/v2/uploads/${row.uuid}/tags`),
      api.get(`/api/v2/uploads/${row.uuid}/collections`),
    ])
    const tagMap = tagsRes.data.tags || {}
    detailTags.value = Object.entries(tagMap).map(([key, value]) => ({ key, value: value as string }))
    const ids: number[] = collRes.data.collection_ids || []
    if (ids.length) {
      if (!collectionNameCache) {
        const cs = await api.get('/api/v2/collections')
        collectionNameCache = {}
        for (const c of (cs.data.collections || []) as Collection[]) collectionNameCache[c.id] = c.name
      }
      detailCollections.value = ids.map((id) => collectionNameCache?.[id] ?? `#${id}`)
    }
  } catch {
    // Core fields still render from the row data even if tags/collections fail.
  } finally {
    detailLoading.value = false
  }
}

async function copyDatamap(text: string) {
  try {
    await navigator.clipboard?.writeText(text)
    if (!navigator.clipboard) throw new Error('clipboard unavailable')
    toast.add({ severity: 'success', summary: 'Copied', detail: 'Address copied to clipboard', life: 2000 })
  } catch {
    toast.add({ severity: 'error', summary: 'Copy failed', detail: 'Select the address and copy manually.', life: 5000 })
  }
}

function fmtDateTime(s?: string): string {
  if (!s) return '—'
  const d = new Date(s)
  return isNaN(d.getTime()) ? '—' : d.toLocaleString()
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
    toast.add({ severity: 'error', summary: 'Download Failed', detail: 'File may not be completed yet', life: 5000 })
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
        toast.add({ severity: 'success', summary: 'Cancelled', detail: 'Upload cancelled', life: 3000 })
        await fetchUploads()
      } catch (e: any) {
        toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to cancel upload', life: 5000 })
      }
    },
  })
}

async function retryUpload(uuid: string) {
  try {
    await api.post(`/api/v2/uploads/${uuid}/retry`)
    toast.add({ severity: 'success', summary: 'Retrying', detail: 'Upload queued for retry', life: 3000 })
    await fetchUploads()
  } catch (e: any) {
    toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to retry upload', life: 5000 })
  }
}

async function forceRetry(uuid: string) {
  try {
    await api.post(`/api/v2/uploads/${uuid}/force-retry`)
    await fetchUploads()
  } catch (e: any) {
    toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to force retry', life: 5000 })
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
        toast.add({ severity: 'success', summary: 'Deleted', detail: 'Upload deleted', life: 3000 })
        await fetchUploads()
      } catch (e: any) {
        toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to delete upload', life: 5000 })
      }
    },
  })
}

// Bulk operations
function bulkDelete() {
  const targets = selectedUploads.value.filter(u => u.status === 'completed' || u.status === 'already_stored' || u.status === 'failed')
  if (!targets.length) return
  confirm.require({
    message: `Delete ${targets.length} upload${targets.length === 1 ? '' : 's'}? This cannot be undone.`,
    header: 'Confirm Bulk Delete',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: async () => {
      bulkProcessing.value = true
      let ok = 0, fail = 0
      for (const u of targets) {
        try {
          await api.delete(`/api/v2/uploads/${u.uuid}`)
          ok++
        } catch {
          fail++
        }
      }
      bulkProcessing.value = false
      selectedUploads.value = []
      toast.add({ severity: ok ? 'success' : 'error', summary: 'Bulk Delete', detail: `${ok} deleted${fail ? `, ${fail} failed` : ''}`, life: 4000 })
      await fetchUploads()
    },
  })
}

async function bulkRetry() {
  const targets = selectedUploads.value.filter(u => u.status === 'failed')
  if (!targets.length) return
  bulkProcessing.value = true
  let ok = 0, fail = 0
  for (const u of targets) {
    try {
      await api.post(`/api/v2/uploads/${u.uuid}/retry`)
      ok++
    } catch {
      fail++
    }
  }
  bulkProcessing.value = false
  selectedUploads.value = []
  toast.add({ severity: ok ? 'success' : 'error', summary: 'Bulk Retry', detail: `${ok} queued for retry${fail ? `, ${fail} failed` : ''}`, life: 4000 })
  await fetchUploads()
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
    toast.add({ severity: 'success', summary: 'Saved', detail: 'Tags updated', life: 3000 })
    tagsDialogVisible.value = false
  } catch (e: any) {
    toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to save tags', life: 5000 })
  } finally {
    tagsSaving.value = false
  }
}

// Add to collection
const collectDialogVisible = ref(false)
const collectUploadUuid = ref('')
const collectUploadName = ref('')
const collections = ref<Collection[]>([])
const loadingCollections = ref(false)
const addingToCollection = ref<number | null>(null)
const addedToCollections = ref<number[]>([])

function isInCollection(id: number): boolean {
  return addedToCollections.value.includes(id)
}

async function openCollections(uuid: string, name: string) {
  collectUploadUuid.value = uuid
  collectUploadName.value = name
  addedToCollections.value = []
  collectDialogVisible.value = true
  loadingCollections.value = true
  try {
    const res = await api.get('/api/v2/collections')
    collections.value = res.data.collections || []
    // Check which collections already contain this file
    const found: number[] = []
    for (const c of collections.value) {
      try {
        const filesRes = await api.get(`/api/v2/collections/${c.id}`)
        const files = filesRes.data.files || []
        if (files.some((f: { uuid: string }) => f.uuid === uuid)) {
          found.push(c.id)
        }
      } catch {
        // ignore per-collection errors
      }
    }
    addedToCollections.value = found
  } catch {
    collections.value = []
  } finally {
    loadingCollections.value = false
  }
}

async function addToCollection(collectionId: number) {
  addingToCollection.value = collectionId
  try {
    await api.post(`/api/v2/collections/${collectionId}/files`, {
      upload_uuid: collectUploadUuid.value,
    })
    addedToCollections.value = [...addedToCollections.value, collectionId]
    toast.add({ severity: 'success', summary: 'Added', detail: 'File added to collection', life: 3000 })
  } catch (e: any) {
    toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to add to collection', life: 5000 })
  } finally {
    addingToCollection.value = null
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
    case 'already_stored': return 'info'
    case 'failed': return 'danger'
    case 'processing': return 'warn'
    default: return 'info'
  }
}

function statusLabel(u: any) {
  if (u.status_detail === 'gas_backoff') {
    return `waiting (gas high, attempt ${u.backoff_attempt})`
  }
  if (u.status === 'already_stored') return 'already on network'
  return u.status
}

function onPage(event: any) {
  page.value = event.page + 1
  fetchUploads()
}

// Lazy DataTable disables client-side sort — handle the event and re-fetch
// sorted from the server. Sorting applies to the full list (the /uploads
// endpoint), so it falls back from any active tag/filename search.
function onSort(event: any) {
  sortField.value = event.sortField || ''
  sortOrder.value = event.sortOrder || -1
  page.value = 1
  fetchUploads()
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
        <form @submit.prevent="handleUpload" class="flex flex-col gap-4">
          <div class="flex flex-col sm:flex-row items-start gap-4">
            <div class="flex-1">
              <input id="upload-file-input" type="file" @change="onFileSelect" :disabled="noWallet"
                class="block w-full text-sm text-gray-500 file:mr-4 file:py-2 file:px-4 file:rounded file:border-0 file:text-sm file:font-medium file:bg-blue-50 dark:file:bg-blue-950/40 file:text-blue-700 dark:file:text-blue-300 hover:file:bg-blue-100 dark:hover:file:bg-blue-900/50" />
            </div>
            <Select v-model="visibility" :options="visibilityOptions" optionLabel="label" optionValue="value"
              :disabled="noWallet" class="w-36" />
            <Button type="submit" :label="uploading ? 'Uploading...' : 'Upload'" icon="pi pi-upload"
              :disabled="!file || uploading || noWallet" :loading="uploading" />
          </div>

          <!-- Upload-time tags -->
          <div v-if="file" class="border-t border-surface-200 pt-3">
            <p class="text-xs text-surface-500 mb-2">Tags (optional — applied when the file is queued)</p>
            <div class="flex flex-wrap gap-1 mb-2" v-if="uploadTags.length">
              <Tag v-for="t in uploadTags" :key="t.key" :value="`${t.key}: ${t.value}`" severity="info" class="cursor-pointer" @click="removeUploadTag(t.key)" v-tooltip.top="'Click to remove'" />
            </div>
            <div class="flex gap-2 items-center">
              <InputText v-model="uploadTagKey" placeholder="Key" size="small" class="w-28" @keyup.enter="addUploadTag" />
              <InputText v-model="uploadTagValue" placeholder="Value" size="small" class="w-28" @keyup.enter="addUploadTag" />
              <Button icon="pi pi-plus" size="small" text rounded @click="addUploadTag" :disabled="!uploadTagKey.trim() || !uploadTagValue.trim()" aria-label="Add tag" />
            </div>
          </div>
        </form>
      </template>
    </Card>

    <!-- Search / filter bar -->
    <Card class="mb-4">
      <template #content>
        <div class="flex flex-wrap gap-3 items-end">
          <div>
            <label class="block text-xs text-gray-500 mb-1">Filename</label>
            <InputText v-model="searchQuery" placeholder="Search filename..." size="small" class="w-48"
              @keyup.enter="runSearch" />
          </div>
          <div>
            <label class="block text-xs text-gray-500 mb-1">Tag Key</label>
            <AutoComplete v-model="tagKey" :suggestions="tagKeySuggestions" @complete="searchTagKeys"
              placeholder="e.g. project" dropdown size="small" class="w-40" />
          </div>
          <div>
            <label class="block text-xs text-gray-500 mb-1">Tag Value</label>
            <AutoComplete v-model="tagValue" :suggestions="tagValueSuggestions" @complete="searchTagValues"
              placeholder="e.g. alpha" dropdown size="small" class="w-40" />
          </div>
          <div>
            <label class="block text-xs text-gray-500 mb-1">Range</label>
            <Select v-model="datePreset" :options="PRESET_OPTIONS" optionLabel="label" optionValue="value"
              size="small" class="w-36" @change="onDatePresetChange" />
          </div>
          <div>
            <label class="block text-xs text-gray-500 mb-1">Since</label>
            <DatePicker v-model="sinceDate" dateFormat="yy-mm-dd" showIcon size="small" class="w-40"
              :disabled="!!datePreset" @date-select="onManualDate" />
          </div>
          <div>
            <label class="block text-xs text-gray-500 mb-1">Until</label>
            <DatePicker v-model="untilDate" dateFormat="yy-mm-dd" showIcon size="small" class="w-40"
              :disabled="!!datePreset" @date-select="onManualDate" />
          </div>
          <Button label="Search" icon="pi pi-search" severity="secondary" size="small" @click="runSearch" />
          <Button label="Clear" text size="small"
            @click="tagKey = ''; tagValue = ''; searchQuery = ''; datePreset = ''; sinceDate = null; untilDate = null; page = 1; fetchUploads()" />
        </div>
      </template>
    </Card>

    <!-- Bulk action bar -->
    <div v-if="selectedUploads.length" class="mb-2 flex items-center gap-3 px-4 py-2 bg-primary-50 rounded-lg border border-primary-200">
      <span class="text-sm font-medium">{{ selectedUploads.length }} selected</span>
      <Button label="Delete" icon="pi pi-trash" severity="danger" size="small" outlined
        :loading="bulkProcessing" :disabled="!selectedUploads.some(u => u.status === 'completed' || u.status === 'already_stored' || u.status === 'failed')"
        @click="bulkDelete" />
      <Button label="Retry" icon="pi pi-refresh" severity="warn" size="small" outlined
        :loading="bulkProcessing" :disabled="!selectedUploads.some(u => u.status === 'failed')"
        @click="bulkRetry" />
      <Button label="Clear" text size="small" @click="selectedUploads = []" />
    </div>

    <!-- Table -->
    <Card>
      <template #content>
        <DataTable :value="uploads" v-model:selection="selectedUploads" :loading="loading" stripedRows
          paginator :rows="limit" :totalRecords="total" :lazy="true" @page="onPage"
          :sortField="sortField" :sortOrder="sortOrder" @sort="onSort"
          paginatorTemplate="FirstPageLink PrevPageLink PageLinks NextPageLink LastPageLink CurrentPageReport"
          currentPageReportTemplate="Showing {first} to {last} of {totalRecords}"
          dataKey="uuid"
          :pt="{ root: { class: '-mt-2' } }">
          <template #empty>No uploads found.</template>
          <Column selectionMode="multiple" headerStyle="width: 3rem" />
          <Column field="original_filename" header="Name" sortable>
            <template #body="{ data }">
              <span :class="['cursor-pointer hover:underline', data.visibility === 'public' ? 'text-amber-600 font-medium' : '']"
                @click="openDetail(data)" v-tooltip.top="'View details'">
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
                <Button v-if="data.status === 'completed' || data.status === 'already_stored'" icon="pi pi-download" label="Download"
                  outlined size="small"
                  :loading="downloading === data.uuid" @click="download(data.uuid, data.original_filename)" />
                <Button v-if="data.status === 'completed' || data.status === 'already_stored'" icon="pi pi-tag" text rounded size="small"
                  severity="secondary" aria-label="Tags" @click="openTags(data.uuid, data.original_filename)" v-tooltip.top="'Tags'" />
                <Button v-if="data.status === 'completed' || data.status === 'already_stored'" icon="pi pi-folder" text rounded size="small"
                  severity="secondary" aria-label="Add to Collection" @click="openCollections(data.uuid, data.original_filename)" v-tooltip.top="'Add to Collection'" />
                <Button v-if="data.status === 'completed' || data.status === 'already_stored'" icon="pi pi-trash" text rounded size="small"
                  severity="secondary" aria-label="Delete" @click="deleteUpload(data.uuid)" v-tooltip.top="'Delete'" />

                <!-- Queued (not backoff): cancel -->
                <Button v-if="data.status === 'queued' && data.status_detail !== 'gas_backoff'"
                  icon="pi pi-times" text rounded size="small" severity="danger"
                  aria-label="Cancel" @click="cancelUpload(data.uuid)" v-tooltip.top="'Cancel'" />

                <!-- Processing: cancel -->
                <Button v-if="data.status === 'processing'" icon="pi pi-times" text rounded size="small"
                  severity="danger" aria-label="Cancel" @click="cancelUpload(data.uuid)" v-tooltip.top="'Cancel'" />

                <!-- Gas backoff: force retry, cancel -->
                <Button v-if="data.status_detail === 'gas_backoff'" icon="pi pi-refresh" text rounded size="small"
                  aria-label="Retry now" @click="forceRetry(data.uuid)" v-tooltip.top="'Retry now'" />
                <Button v-if="data.status_detail === 'gas_backoff'" icon="pi pi-times" text rounded size="small"
                  severity="danger" aria-label="Cancel" @click="cancelUpload(data.uuid)" v-tooltip.top="'Cancel'" />

                <!-- Failed: retry, delete -->
                <Button v-if="data.status === 'failed'" icon="pi pi-refresh" text rounded size="small"
                  aria-label="Retry" @click="retryUpload(data.uuid)" v-tooltip.top="'Retry'" />
                <Button v-if="data.status === 'failed'" icon="pi pi-trash" text rounded size="small"
                  severity="danger" aria-label="Delete" @click="deleteUpload(data.uuid)" v-tooltip.top="'Delete'" />
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
            <Button icon="pi pi-times" text rounded size="small" severity="danger" aria-label="Remove tag"
              v-tooltip.top="'Remove'" @click="removeTag(t.key)" />
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

    <!-- Add to collection dialog -->
    <Dialog v-model:visible="collectDialogVisible" :header="'Add to Collection: ' + collectUploadName" modal :style="{ width: '28rem' }">
      <div v-if="loadingCollections" class="flex flex-col gap-2 py-2">
        <Skeleton v-for="i in 3" :key="i" height="3rem" borderRadius="8px" />
      </div>
      <div v-else-if="collections.length === 0" class="py-4 text-center text-surface-400">
        No collections yet. Create one on the <router-link to="/collections" class="text-primary hover:underline">Collections page</router-link>.
      </div>
      <div v-else class="flex flex-col gap-1">
        <div v-for="c in collections" :key="c.id"
          class="flex items-center justify-between px-3 py-2 rounded-lg hover:bg-surface-50">
          <div>
            <p class="text-sm font-medium">{{ c.name }}</p>
            <p class="text-xs text-surface-400">{{ c.file_count || 0 }} files</p>
          </div>
          <Button v-if="isInCollection(c.id)" icon="pi pi-check" size="small" text severity="success"
            disabled aria-label="Already in this collection" v-tooltip.top="'Already in this collection'" />
          <Button v-else label="Add" icon="pi pi-plus" size="small" outlined
            :loading="addingToCollection === c.id" @click="addToCollection(c.id)" />
        </div>
      </div>
    </Dialog>

    <!-- Per-file detail panel (V2-403) -->
    <Drawer v-model:visible="detailVisible" position="right" :style="{ width: '30rem' }"
      :header="detail?.original_filename || 'File details'">
      <div v-if="detail" class="flex flex-col gap-5 text-sm">
        <!-- Identity -->
        <section>
          <h3 class="text-xs font-semibold uppercase text-surface-400 mb-2">Identity</h3>
          <dl class="flex flex-col gap-2">
            <div class="flex justify-between gap-3">
              <dt class="text-surface-500">UUID</dt>
              <dd class="font-mono text-surface-700 truncate flex items-center gap-1 min-w-0">
                <span class="truncate">{{ detail.uuid }}</span>
                <Button icon="pi pi-copy" text rounded size="small" aria-label="Copy UUID"
                  v-tooltip.top="'Copy'" @click="copyDatamap(detail.uuid)" />
              </dd>
            </div>
            <div class="flex justify-between gap-3"><dt class="text-surface-500">Type</dt><dd>{{ detail.content_type || '—' }}</dd></div>
            <div class="flex justify-between gap-3">
              <dt class="text-surface-500">Visibility</dt>
              <dd><Tag :value="detail.visibility" :severity="detail.visibility === 'public' ? 'warn' : 'secondary'" /></dd>
            </div>
            <div class="flex justify-between gap-3"><dt class="text-surface-500">Size</dt><dd>{{ formatSize(detail.file_size) }}</dd></div>
          </dl>
        </section>

        <!-- Network address -->
        <section>
          <h3 class="text-xs font-semibold uppercase text-surface-400 mb-2">Network address</h3>
          <div v-if="detail.datamap_address" class="flex flex-col gap-1.5">
            <code class="bg-surface-100 px-2 py-1 rounded font-mono text-xs break-all">{{ detail.datamap_address }}</code>
            <div class="flex items-center gap-3">
              <Button icon="pi pi-copy" label="Copy address" text size="small" @click="copyDatamap(detail.datamap_address)" />
              <a v-if="detail.visibility === 'public'" :href="`autonomi://${detail.datamap_address}`"
                class="text-primary text-xs hover:underline">autonomi:// link</a>
            </div>
          </div>
          <p v-else class="text-surface-400 text-xs">No network address yet — set once the upload completes.</p>
        </section>

        <!-- Status & timeline -->
        <section>
          <h3 class="text-xs font-semibold uppercase text-surface-400 mb-2">Status</h3>
          <dl class="flex flex-col gap-2">
            <div class="flex justify-between gap-3">
              <dt class="text-surface-500">Status</dt>
              <dd><Tag :value="statusLabel(detail)" :severity="statusSeverity(detail.status, detail.status_detail)" /></dd>
            </div>
            <div class="flex justify-between gap-3"><dt class="text-surface-500">Created</dt><dd>{{ fmtDateTime(detail.created_at) }}</dd></div>
            <div class="flex justify-between gap-3"><dt class="text-surface-500">Queued</dt><dd>{{ fmtDateTime(detail.queued_at) }}</dd></div>
            <div v-if="detail.processing_at" class="flex justify-between gap-3"><dt class="text-surface-500">Processing</dt><dd>{{ fmtDateTime(detail.processing_at) }}</dd></div>
            <div v-if="detail.completed_at" class="flex justify-between gap-3"><dt class="text-surface-500">Completed</dt><dd>{{ fmtDateTime(detail.completed_at) }}</dd></div>
            <div v-if="detail.failed_at" class="flex justify-between gap-3"><dt class="text-surface-500">Failed</dt><dd>{{ fmtDateTime(detail.failed_at) }}</dd></div>
            <div v-if="detail.backoff_attempt" class="flex justify-between gap-3"><dt class="text-surface-500">Backoff attempts</dt><dd>{{ detail.backoff_attempt }}</dd></div>
          </dl>
          <div v-if="detail.error_message" class="mt-2 p-2 rounded bg-red-50 dark:bg-red-950/40 text-red-700 dark:text-red-300 text-xs break-words">
            {{ detail.error_message }}
          </div>
        </section>

        <!-- Cost -->
        <section>
          <h3 class="text-xs font-semibold uppercase text-surface-400 mb-2">Cost</h3>
          <dl class="flex flex-col gap-2">
            <div class="flex justify-between gap-3"><dt class="text-surface-500">Estimated</dt><dd>{{ detail.estimated_cost || '—' }}</dd></div>
            <div class="flex justify-between gap-3"><dt class="text-surface-500">Actual</dt><dd>{{ detail.actual_cost || '—' }}</dd></div>
            <div v-if="detail.last_quoted_cost" class="flex justify-between gap-3"><dt class="text-surface-500">Last quoted</dt><dd>{{ detail.last_quoted_cost }}</dd></div>
          </dl>
        </section>

        <!-- Tags -->
        <section>
          <h3 class="text-xs font-semibold uppercase text-surface-400 mb-2">Tags</h3>
          <div v-if="detailLoading" class="text-surface-400 text-xs">Loading…</div>
          <div v-else-if="detailTags.length" class="flex flex-wrap gap-1.5">
            <Tag v-for="t in detailTags" :key="t.key" :value="`${t.key}: ${t.value}`" severity="info" />
          </div>
          <p v-else class="text-surface-400 text-xs">No tags.</p>
        </section>

        <!-- Collections -->
        <section>
          <h3 class="text-xs font-semibold uppercase text-surface-400 mb-2">Collections</h3>
          <div v-if="detailLoading" class="text-surface-400 text-xs">Loading…</div>
          <div v-else-if="detailCollections.length" class="flex flex-wrap gap-1.5">
            <Tag v-for="c in detailCollections" :key="c" :value="c" severity="secondary" />
          </div>
          <p v-else class="text-surface-400 text-xs">Not in any collection.</p>
        </section>
      </div>
    </Drawer>
  </div>
</template>
