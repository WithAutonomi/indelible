<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import { api } from '../../api/client'
import type { Collection, CollectionFile, Upload } from '../../types/api'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Dialog from 'primevue/dialog'
import Card from 'primevue/card'
import Skeleton from 'primevue/skeleton'

const confirm = useConfirm()
const toast = useToast()

const collections = ref<Collection[]>([])
const loading = ref(true)
const showCreate = ref(false)
const newName = ref('')
const newDescription = ref('')
const creating = ref(false)

const selectedCollection = ref<Collection | null>(null)
const collectionFiles = ref<CollectionFile[]>([])
const loadingFiles = ref(false)
const filesPage = ref(1)
const filesTotal = ref(0)
const filesLimit = 20

async function fetchCollections() {
  loading.value = true
  try {
    const res = await api.get('/api/v2/collections')
    collections.value = res.data.collections || []
  } catch {
    // ignore
  } finally {
    loading.value = false
  }
}

async function createCollection() {
  creating.value = true
  try {
    await api.post('/api/v2/collections', {
      name: newName.value,
      description: newDescription.value,
    })
    newName.value = ''
    newDescription.value = ''
    showCreate.value = false
    await fetchCollections()
    toast.add({ severity: 'success', summary: 'Created', detail: 'Collection created', life: 3000 })
  } catch (e: any) {
    toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to create collection', life: 5000 })
  } finally {
    creating.value = false
  }
}

async function selectCollection(c: Collection) {
  selectedCollection.value = c
  filesPage.value = 1
  await fetchCollectionFiles()
}

async function fetchCollectionFiles() {
  if (!selectedCollection.value) return
  loadingFiles.value = true
  try {
    const res = await api.get(`/api/v2/collections/${selectedCollection.value.id}`, {
      params: { limit: filesLimit, offset: (filesPage.value - 1) * filesLimit },
    })
    collectionFiles.value = res.data.files || []
    filesTotal.value = res.data.total_files || 0
  } catch {
    collectionFiles.value = []
    filesTotal.value = 0
  } finally {
    loadingFiles.value = false
  }
}

function onFilesPage(event: any) {
  filesPage.value = event.page + 1
  fetchCollectionFiles()
}

function deleteCollection(id: number) {
  confirm.require({
    message: 'Delete this collection? This cannot be undone.',
    header: 'Confirm Delete',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: async () => {
      try {
        await api.delete(`/api/v2/collections/${id}`)
        if (selectedCollection.value?.id === id) {
          selectedCollection.value = null
        }
        await fetchCollections()
        toast.add({ severity: 'success', summary: 'Deleted', detail: 'Collection deleted', life: 3000 })
      } catch {
        toast.add({ severity: 'error', summary: 'Error', detail: 'Failed to delete collection', life: 5000 })
      }
    },
  })
}

// Add file to collection
const showAddFile = ref(false)
const availableUploads = ref<Upload[]>([])
const loadingUploads = ref(false)
const addingFile = ref<string | null>(null)

async function openAddFile() {
  showAddFile.value = true
  loadingUploads.value = true
  try {
    const res = await api.get('/api/v2/uploads', { params: { limit: 100 } })
    const uploads = res.data.uploads || []
    // Filter to completed uploads not already in the collection
    const existingIds = new Set(collectionFiles.value.map((f: CollectionFile) => f.uuid))
    availableUploads.value = uploads.filter((u: Upload) => (u.status === 'completed' || u.status === 'already_stored') && !existingIds.has(u.uuid))
  } catch {
    availableUploads.value = []
  } finally {
    loadingUploads.value = false
  }
}

async function addFileToCollection(uploadUuid: string) {
  if (!selectedCollection.value) return
  addingFile.value = uploadUuid
  try {
    await api.post(`/api/v2/collections/${selectedCollection.value.id}/files`, {
      upload_uuid: uploadUuid,
    })
    // Refresh the collection files and available list
    availableUploads.value = availableUploads.value.filter((u: Upload) => u.uuid !== uploadUuid)
    await selectCollection(selectedCollection.value)
    await fetchCollections()
    toast.add({ severity: 'success', summary: 'Added', detail: 'File added to collection', life: 3000 })
  } catch (e: any) {
    toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to add file', life: 5000 })
  } finally {
    addingFile.value = null
  }
}

function removeFile(uploadUuid: string) {
  if (!selectedCollection.value) return
  confirm.require({
    message: 'Remove this file from the collection?',
    header: 'Confirm Remove',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: async () => {
      try {
        await api.delete(`/api/v2/collections/${selectedCollection.value!.id}/files/${uploadUuid}`)
        await fetchCollectionFiles()
        await fetchCollections()
        toast.add({ severity: 'success', summary: 'Removed', detail: 'File removed from collection', life: 3000 })
      } catch {
        toast.add({ severity: 'error', summary: 'Error', detail: 'Failed to remove file', life: 5000 })
      }
    },
  })
}

onMounted(fetchCollections)
</script>

<template>
  <div class="p-6">

    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold">Collections</h1>
      <Button label="New Collection" icon="pi pi-plus" @click="showCreate = true" />
    </div>

    <!-- Create collection dialog -->
    <Dialog v-model:visible="showCreate" header="New Collection" modal :style="{ width: '28rem' }">
      <form @submit.prevent="createCollection" class="flex flex-col gap-4 pt-2">
        <div>
          <label class="block text-sm font-medium mb-1">Name</label>
          <InputText v-model="newName" required placeholder="Collection name" class="w-full" />
        </div>
        <div>
          <label class="block text-sm font-medium mb-1">Description</label>
          <InputText v-model="newDescription" placeholder="Optional description" class="w-full" />
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <Button label="Cancel" severity="secondary" text @click="showCreate = false" />
          <Button type="submit" :label="creating ? 'Creating...' : 'Create'" :loading="creating" />
        </div>
      </form>
    </Dialog>

    <div class="grid grid-cols-1 lg:grid-cols-3 gap-6">
      <!-- Collection list -->
      <div class="lg:col-span-1">
        <Card>
          <template #content>
            <div v-if="loading" class="flex flex-col gap-3 p-4">
              <Skeleton v-for="i in 4" :key="i" height="2.5rem" borderRadius="8px" />
            </div>
            <div v-else-if="collections.length === 0" class="p-4 text-center text-gray-400">No collections yet.</div>
            <div v-else class="flex flex-col -mt-2">
              <div v-for="c in collections" :key="c.id"
                @click="selectCollection(c)"
                class="px-4 py-3 cursor-pointer hover:bg-gray-50 flex items-center justify-between rounded-lg transition-colors"
                :class="selectedCollection?.id === c.id ? 'bg-primary-50' : ''">
                <div>
                  <p class="text-sm font-medium">{{ c.name }}</p>
                  <p class="text-xs text-gray-400">{{ c.file_count || 0 }} files</p>
                </div>
                <Button icon="pi pi-trash" text rounded size="small" severity="danger" aria-label="Delete collection"
                  v-tooltip.top="'Delete'" @click.stop="deleteCollection(c.id)" />
              </div>
            </div>
          </template>
        </Card>
      </div>

      <!-- Collection files -->
      <div class="lg:col-span-2">
        <Card>
          <template #content>
            <div v-if="!selectedCollection" class="p-6 text-center text-gray-400">
              Select a collection to view its files.
            </div>
            <template v-else>
              <div class="flex items-center justify-between mb-4">
                <div>
                  <h2 class="text-lg font-semibold">{{ selectedCollection.name }}</h2>
                  <p v-if="selectedCollection.description" class="text-sm text-gray-500">{{ selectedCollection.description }}</p>
                </div>
                <Button label="Add File" icon="pi pi-plus" size="small" outlined @click="openAddFile" />
              </div>
              <DataTable :value="collectionFiles" :loading="loadingFiles" stripedRows
                paginator :rows="filesLimit" :totalRecords="filesTotal" :lazy="true" @page="onFilesPage"
                paginatorTemplate="FirstPageLink PrevPageLink PageLinks NextPageLink LastPageLink CurrentPageReport"
                currentPageReportTemplate="Showing {first} to {last} of {totalRecords}">
                <template #empty>No files in this collection yet.</template>
                <Column field="original_filename" header="Name">
                  <template #body="{ data }">
                    <span class="font-medium">{{ data.original_filename || data.uuid }}</span>
                  </template>
                </Column>
                <Column field="added_at" header="Added">
                  <template #body="{ data }">
                    <span class="text-gray-400">{{ data.added_at ? new Date(data.added_at).toLocaleDateString() : '—' }}</span>
                  </template>
                </Column>
                <Column header="Actions" style="width: 5rem">
                  <template #body="{ data }">
                    <Button icon="pi pi-times" text rounded size="small" severity="secondary"
                      aria-label="Remove from collection" v-tooltip.top="'Remove from collection'"
                      @click="removeFile(data.uuid)" />
                  </template>
                </Column>
              </DataTable>
            </template>
          </template>
        </Card>
      </div>
    </div>

    <!-- Add file dialog -->
    <Dialog v-model:visible="showAddFile" header="Add File to Collection" modal :style="{ width: '34rem' }">
      <DataTable :value="availableUploads" :loading="loadingUploads" stripedRows :rows="10" paginator
        paginatorTemplate="FirstPageLink PrevPageLink PageLinks NextPageLink LastPageLink CurrentPageReport"
        currentPageReportTemplate="Showing {first} to {last} of {totalRecords}"
        :pt="{ root: { class: '-mt-2' } }">
        <template #empty>No completed uploads available to add.</template>
        <Column field="original_filename" header="File" sortable>
          <template #body="{ data }">
            <span class="text-sm font-medium">{{ data.original_filename }}</span>
          </template>
        </Column>
        <Column header="" style="width: 6rem">
          <template #body="{ data }">
            <Button label="Add" icon="pi pi-plus" size="small" outlined
              :loading="addingFile === data.uuid" @click="addFileToCollection(data.uuid)" />
          </template>
        </Column>
      </DataTable>
    </Dialog>
  </div>
</template>
