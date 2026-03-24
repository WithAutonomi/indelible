<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '../../api/client'

const collections = ref<any[]>([])
const loading = ref(true)
const showCreate = ref(false)
const newName = ref('')
const newDescription = ref('')
const creating = ref(false)

const selectedCollection = ref<any>(null)
const collectionFiles = ref<any[]>([])
const loadingFiles = ref(false)

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
  } catch (e: any) {
    alert(e.response?.data?.error || 'Failed to create collection')
  } finally {
    creating.value = false
  }
}

async function selectCollection(c: any) {
  selectedCollection.value = c
  loadingFiles.value = true
  try {
    const res = await api.get(`/api/v2/collections/${c.id}`)
    collectionFiles.value = res.data.files || []
  } catch {
    collectionFiles.value = []
  } finally {
    loadingFiles.value = false
  }
}

async function deleteCollection(id: number) {
  if (!confirm('Delete this collection? This cannot be undone.')) return
  try {
    await api.delete(`/api/v2/collections/${id}`)
    if (selectedCollection.value?.id === id) {
      selectedCollection.value = null
    }
    await fetchCollections()
  } catch {
    alert('Failed to delete collection')
  }
}

async function removeFile(uploadId: string) {
  if (!selectedCollection.value) return
  try {
    await api.delete(`/api/v2/collections/${selectedCollection.value.id}/files/${uploadId}`)
    collectionFiles.value = collectionFiles.value.filter((f: any) => f.upload_uuid !== uploadId)
  } catch {
    alert('Failed to remove file')
  }
}

onMounted(fetchCollections)
</script>

<template>
  <div class="p-6">
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold">Collections</h1>
      <button @click="showCreate = !showCreate"
        class="rounded bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700">
        <i class="pi pi-plus mr-1"></i> New Collection
      </button>
    </div>

    <!-- Create form -->
    <div v-if="showCreate" class="bg-white rounded-lg border border-gray-200 mb-6">
      <div class="px-6 py-4 border-b border-gray-200">
        <h2 class="text-base font-semibold text-gray-800">New Collection</h2>
      </div>
      <form @submit.prevent="createCollection">
        <div class="divide-y divide-gray-100">
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium text-gray-700">Name</label>
              <p class="text-xs text-gray-400 mt-1">A name for this collection.</p>
            </div>
            <div class="col-span-2">
              <input v-model="newName" type="text" required
                class="block w-full max-w-md rounded border border-gray-300 px-3 py-2 text-sm" />
            </div>
          </div>
          <div class="grid grid-cols-3 gap-6 px-6 py-5">
            <div>
              <label class="text-sm font-medium text-gray-700">Description</label>
              <p class="text-xs text-gray-400 mt-1">Optional description for this collection.</p>
            </div>
            <div class="col-span-2">
              <input v-model="newDescription" type="text"
                class="block w-full max-w-md rounded border border-gray-300 px-3 py-2 text-sm" />
            </div>
          </div>
        </div>
        <div class="px-6 py-4 bg-gray-50 border-t border-gray-200 rounded-b-lg flex justify-end gap-2">
          <button type="button" @click="showCreate = false"
            class="rounded border px-3 py-1.5 text-sm text-gray-600 hover:bg-gray-100">Cancel</button>
          <button type="submit" :disabled="creating"
            class="rounded bg-blue-600 px-4 py-1.5 text-sm text-white hover:bg-blue-700 disabled:opacity-50">
            {{ creating ? 'Creating...' : 'Create' }}
          </button>
        </div>
      </form>
    </div>

    <div class="grid grid-cols-1 lg:grid-cols-3 gap-6">
      <!-- Collection list -->
      <div class="lg:col-span-1">
        <div class="bg-white rounded-lg border border-gray-200">
          <div v-if="loading" class="p-4 text-center text-gray-400">Loading...</div>
          <div v-else-if="collections.length === 0" class="p-4 text-center text-gray-400">No collections yet.</div>
          <div v-else class="divide-y divide-gray-100">
            <div v-for="c in collections" :key="c.id"
              @click="selectCollection(c)"
              class="px-4 py-3 cursor-pointer hover:bg-gray-50 flex items-center justify-between"
              :class="selectedCollection?.id === c.id ? 'bg-blue-50' : ''">
              <div>
                <p class="text-sm font-medium text-gray-800">{{ c.name }}</p>
                <p class="text-xs text-gray-400">{{ c.file_count || 0 }} files</p>
              </div>
              <button @click.stop="deleteCollection(c.id)"
                class="text-gray-300 hover:text-red-500 text-sm">
                <i class="pi pi-trash"></i>
              </button>
            </div>
          </div>
        </div>
      </div>

      <!-- Collection files -->
      <div class="lg:col-span-2">
        <div class="bg-white rounded-lg border border-gray-200">
          <div v-if="!selectedCollection" class="p-6 text-center text-gray-400">
            Select a collection to view its files.
          </div>
          <template v-else>
            <div class="px-6 py-4 border-b border-gray-200">
              <h2 class="text-lg font-semibold">{{ selectedCollection.name }}</h2>
              <p v-if="selectedCollection.description" class="text-sm text-gray-500">{{ selectedCollection.description }}</p>
            </div>
            <div v-if="loadingFiles" class="p-6 text-center text-gray-400">Loading files...</div>
            <div v-else-if="collectionFiles.length === 0" class="p-6 text-center text-gray-400">
              No files in this collection. Add files from the Uploads page.
            </div>
            <table v-else class="w-full">
              <thead class="text-left text-xs text-gray-500 uppercase bg-gray-50">
                <tr>
                  <th class="px-6 py-3">Name</th>
                  <th class="px-6 py-3">Added</th>
                  <th class="px-6 py-3">Actions</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100">
                <tr v-for="f in collectionFiles" :key="f.upload_uuid">
                  <td class="px-6 py-3 text-sm font-medium text-gray-800">{{ f.original_name || f.upload_uuid }}</td>
                  <td class="px-6 py-3 text-sm text-gray-400">{{ new Date(f.added_at).toLocaleDateString() }}</td>
                  <td class="px-6 py-3">
                    <button @click="removeFile(f.upload_uuid)"
                      class="text-red-600 hover:text-red-800 text-sm">Remove</button>
                  </td>
                </tr>
              </tbody>
            </table>
          </template>
        </div>
      </div>
    </div>
  </div>
</template>
