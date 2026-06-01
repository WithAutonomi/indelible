<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import { api } from '../../api/client'
import type { Group, GroupMember, User } from '../../types/api'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Textarea from 'primevue/textarea'
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import Dialog from 'primevue/dialog'
import Drawer from 'primevue/drawer'

const confirm = useConfirm()
const toast = useToast()

const groups = ref<Group[]>([])
const loading = ref(true)

const showCreateForm = ref(false)
const newName = ref('')
const newDescription = ref('')
const newPermission = ref<'read' | 'write' | 'admin'>('read')
const creating = ref(false)

const drawerVisible = ref(false)
const activeGroup = ref<Group | null>(null)
const members = ref<GroupMember[]>([])
const loadingMembers = ref(false)

const allUsers = ref<User[]>([])
const selectedUserToAdd = ref<User | null>(null)
const addingMember = ref(false)

const permissionOptions = [
  { label: 'Read', value: 'read' },
  { label: 'Write', value: 'write' },
  { label: 'Admin', value: 'admin' },
]

const eligibleUsersToAdd = computed(() => {
  const memberIds = new Set(members.value.map(m => m.id))
  return allUsers.value.filter(u => !memberIds.has(u.id) && u.is_active)
})

function formatDate(d: string): string {
  return new Date(d).toLocaleDateString()
}

function permissionSeverity(level: string): string {
  if (level === 'admin') return 'danger'
  if (level === 'write') return 'warn'
  return 'info'
}

function truncate(s: string, n = 24): string {
  return s.length > n ? s.slice(0, n) + '…' : s
}

function copyExternalId(extId: string) {
  navigator.clipboard.writeText(extId)
  toast.add({ severity: 'success', summary: 'Copied', detail: 'External ID copied', life: 2000 })
}

async function fetchGroups() {
  loading.value = true
  try {
    const res = await api.get('/api/v2/admin/groups')
    groups.value = res.data.groups || []
  } catch {
    toast.add({ severity: 'error', summary: 'Error', detail: 'Failed to load groups', life: 5000 })
  } finally {
    loading.value = false
  }
}

async function fetchUsers() {
  try {
    const res = await api.get('/api/v2/admin/users')
    allUsers.value = res.data.users || []
  } catch {
    // ignore — add-member dropdown will just be empty
  }
}

async function createGroup() {
  if (!newName.value.trim()) return
  creating.value = true
  try {
    await api.post('/api/v2/admin/groups', {
      name: newName.value.trim(),
      description: newDescription.value.trim(),
      permission_level: newPermission.value,
    })
    newName.value = ''
    newDescription.value = ''
    newPermission.value = 'read'
    showCreateForm.value = false
    await fetchGroups()
    toast.add({ severity: 'success', summary: 'Created', detail: 'Group created', life: 3000 })
  } catch (e: any) {
    const msg = e.response?.data?.error || 'Failed to create group'
    toast.add({ severity: 'error', summary: 'Error', detail: msg, life: 5000 })
  } finally {
    creating.value = false
  }
}

function deleteGroup(g: Group) {
  confirm.require({
    message: `Delete group "${g.name}"? Members will be removed from the group.`,
    header: 'Confirm Delete',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: async () => {
      try {
        await api.delete(`/api/v2/admin/groups/${g.id}`)
        if (activeGroup.value?.id === g.id) {
          drawerVisible.value = false
          activeGroup.value = null
        }
        await fetchGroups()
        toast.add({ severity: 'success', summary: 'Deleted', detail: 'Group deleted', life: 3000 })
      } catch {
        toast.add({ severity: 'error', summary: 'Error', detail: 'Failed to delete group', life: 5000 })
      }
    },
  })
}

async function openDetail(g: Group) {
  activeGroup.value = g
  drawerVisible.value = true
  await Promise.all([fetchMembers(g.id), fetchUsers()])
}

async function fetchMembers(groupId: number) {
  loadingMembers.value = true
  try {
    const res = await api.get(`/api/v2/admin/groups/${groupId}/members`)
    members.value = res.data.members || []
  } catch {
    members.value = []
    toast.add({ severity: 'error', summary: 'Error', detail: 'Failed to load members', life: 5000 })
  } finally {
    loadingMembers.value = false
  }
}

async function addMember() {
  if (!activeGroup.value || !selectedUserToAdd.value) return
  addingMember.value = true
  try {
    await api.post(`/api/v2/admin/groups/${activeGroup.value.id}/members`, {
      user_id: selectedUserToAdd.value.id,
    })
    selectedUserToAdd.value = null
    await fetchMembers(activeGroup.value.id)
    await fetchGroups()
    toast.add({ severity: 'success', summary: 'Added', detail: 'Member added', life: 3000 })
  } catch (e: any) {
    const msg = e.response?.data?.error || 'Failed to add member'
    toast.add({ severity: 'error', summary: 'Error', detail: msg, life: 5000 })
  } finally {
    addingMember.value = false
  }
}

function removeMember(m: GroupMember) {
  if (!activeGroup.value) return
  const groupId = activeGroup.value.id
  confirm.require({
    message: `Remove ${m.email} from this group?`,
    header: 'Confirm Remove',
    icon: 'pi pi-exclamation-triangle',
    acceptClass: 'p-button-danger',
    accept: async () => {
      try {
        await api.delete(`/api/v2/admin/groups/${groupId}/members/${m.id}`)
        await fetchMembers(groupId)
        await fetchGroups()
        toast.add({ severity: 'success', summary: 'Removed', detail: 'Member removed', life: 3000 })
      } catch {
        toast.add({ severity: 'error', summary: 'Error', detail: 'Failed to remove member', life: 5000 })
      }
    },
  })
}

onMounted(fetchGroups)
</script>

<template>
  <div class="p-6">

    <div class="flex items-center justify-between mb-6">
      <div>
        <h1 class="text-2xl font-bold">Groups</h1>
        <p class="text-sm text-surface-500 mt-1">
          Permission groups. SCIM-provisioned groups sync from your identity provider; local groups are managed here.
        </p>
      </div>
      <Button label="New Group" icon="pi pi-plus" @click="showCreateForm = true" />
    </div>

    <!-- Create dialog -->
    <Dialog v-model:visible="showCreateForm" header="New Group" modal :style="{ width: '32rem' }">
      <div class="space-y-4">
        <div>
          <label class="text-sm font-medium block mb-1">Name</label>
          <InputText v-model="newName" class="w-full" placeholder="Engineering" @keydown.enter.prevent="createGroup" />
        </div>
        <div>
          <label class="text-sm font-medium block mb-1">Description</label>
          <Textarea v-model="newDescription" rows="2" class="w-full" placeholder="Optional" />
        </div>
        <div>
          <label class="text-sm font-medium block mb-1">Permission Level</label>
          <Select v-model="newPermission" :options="permissionOptions" optionLabel="label" optionValue="value" class="w-full" />
        </div>
      </div>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="showCreateForm = false" />
        <Button label="Create" :loading="creating" :disabled="!newName.trim()" @click="createGroup" />
      </template>
    </Dialog>

    <!-- Groups list -->
    <div class="bg-surface-0 rounded-lg border border-surface-200">
      <DataTable :value="groups" :loading="loading" stripedRows>
        <template #empty>No groups yet. Click "New Group" to create one.</template>
        <Column field="name" header="Name" sortable>
          <template #body="{ data }">
            <button class="text-left hover:underline" @click="openDetail(data)">
              <div class="font-medium text-surface-800">{{ data.name }}</div>
              <div v-if="data.description" class="text-xs text-surface-500">{{ data.description }}</div>
            </button>
          </template>
        </Column>
        <Column field="member_count" header="Members" sortable style="width: 7rem">
          <template #body="{ data }">
            <Tag :value="data.member_count" severity="secondary" />
          </template>
        </Column>
        <Column header="Source" style="width: 8rem">
          <template #body="{ data }">
            <Tag v-if="data.external_id" value="SCIM" severity="info" />
            <Tag v-else value="Local" severity="secondary" />
          </template>
        </Column>
        <Column field="external_id" header="External ID" style="width: 16rem">
          <template #body="{ data }">
            <button v-if="data.external_id"
              class="font-mono text-xs text-surface-600 hover:text-primary"
              :title="data.external_id"
              @click="copyExternalId(data.external_id)">
              {{ truncate(data.external_id, 18) }}
              <i class="pi pi-copy ml-1 text-xs"></i>
            </button>
            <span v-else class="text-xs text-surface-400">--</span>
          </template>
        </Column>
        <Column field="permission_level" header="Permission" sortable style="width: 8rem">
          <template #body="{ data }">
            <Tag :value="data.permission_level" :severity="permissionSeverity(data.permission_level)" />
          </template>
        </Column>
        <Column field="created_at" header="Created" sortable style="width: 8rem">
          <template #body="{ data }">
            <span class="text-surface-500 text-sm whitespace-nowrap">{{ formatDate(data.created_at) }}</span>
          </template>
        </Column>
        <Column header="Actions" style="width: 8rem">
          <template #body="{ data }">
            <div class="flex items-center gap-1">
              <Button icon="pi pi-users" severity="secondary" text rounded size="small"
                aria-label="View members" v-tooltip.top="'View members'" @click="openDetail(data)" />
              <Button icon="pi pi-trash" severity="danger" text rounded size="small"
                aria-label="Delete" v-tooltip.top="'Delete'" @click="deleteGroup(data)" />
            </div>
          </template>
        </Column>
      </DataTable>
    </div>

    <!-- Detail drawer -->
    <Drawer v-model:visible="drawerVisible" position="right" class="w-full max-w-2xl">
      <template #header>
        <div>
          <h2 class="text-lg font-bold">{{ activeGroup?.name }}</h2>
          <p v-if="activeGroup?.description" class="text-sm text-surface-500">{{ activeGroup.description }}</p>
        </div>
      </template>

      <div v-if="activeGroup" class="space-y-6">
        <!-- Metadata header -->
        <div class="grid grid-cols-2 gap-4 p-4 bg-surface-50 rounded-lg border border-surface-200">
          <div>
            <div class="text-xs uppercase text-surface-500 font-medium">Permission</div>
            <Tag :value="activeGroup.permission_level" :severity="permissionSeverity(activeGroup.permission_level)" class="mt-1" />
          </div>
          <div>
            <div class="text-xs uppercase text-surface-500 font-medium">Source</div>
            <Tag v-if="activeGroup.external_id" value="SCIM" severity="info" class="mt-1" />
            <Tag v-else value="Local" severity="secondary" class="mt-1" />
          </div>
          <div v-if="activeGroup.external_id" class="col-span-2">
            <div class="text-xs uppercase text-surface-500 font-medium">External ID</div>
            <code class="text-xs font-mono break-all">{{ activeGroup.external_id }}</code>
          </div>
          <div>
            <div class="text-xs uppercase text-surface-500 font-medium">Created</div>
            <div class="text-sm mt-1">{{ formatDate(activeGroup.created_at) }}</div>
          </div>
          <div>
            <div class="text-xs uppercase text-surface-500 font-medium">Members</div>
            <div class="text-sm mt-1">{{ members.length }}</div>
          </div>
        </div>

        <!-- Add member -->
        <div v-if="!activeGroup.external_id" class="flex items-end gap-2">
          <div class="flex-1">
            <label class="text-sm font-medium block mb-1">Add member</label>
            <Select
              v-model="selectedUserToAdd"
              :options="eligibleUsersToAdd"
              optionLabel="email"
              placeholder="Select a user"
              filter
              class="w-full"
            />
          </div>
          <Button label="Add" :loading="addingMember" :disabled="!selectedUserToAdd" @click="addMember" />
        </div>
        <div v-else class="text-xs text-surface-500 bg-surface-50 rounded p-3 border border-surface-200">
          <i class="pi pi-info-circle mr-1"></i>
          SCIM-managed groups can't be edited here. Membership changes must come from your identity provider.
        </div>

        <!-- Members table -->
        <div>
          <h3 class="text-sm font-semibold mb-2">Members</h3>
          <DataTable :value="members" :loading="loadingMembers" stripedRows size="small">
            <template #empty>No members yet.</template>
            <Column field="email" header="Email" sortable>
              <template #body="{ data }">
                <span class="font-mono text-sm">{{ data.email }}</span>
              </template>
            </Column>
            <Column field="name" header="Name" sortable>
              <template #body="{ data }">
                <span class="text-sm">{{ data.name || '--' }}</span>
              </template>
            </Column>
            <Column header="" style="width: 4rem">
              <template #body="{ data }">
                <Button v-if="!activeGroup?.external_id"
                  icon="pi pi-times" severity="danger" text rounded size="small"
                  aria-label="Remove member" v-tooltip.top="'Remove member'"
                  @click="removeMember(data)" />
              </template>
            </Column>
          </DataTable>
        </div>
      </div>
    </Drawer>
  </div>
</template>
