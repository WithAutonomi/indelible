<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useToast } from 'primevue/usetoast'
import { api } from '../../api/client'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Password from 'primevue/password'
import Select from 'primevue/select'
import ToggleSwitch from 'primevue/toggleswitch'
import Dialog from 'primevue/dialog'
import Tag from 'primevue/tag'

const toast = useToast()

const oidcProviders = ref<any[]>([])
const adminGroups = ref<{ id: number; name: string }[]>([])
const savingProviderID = ref<number | null>(null)
const savingExtraParamsID = ref<number | null>(null)

// Create dialog state
const showCreate = ref(false)
const creating = ref(false)
const createDraft = reactive({
  name: '',
  display_name: '',
  issuer_url: '',
  client_id: '',
  client_secret: '',
  scopes: 'openid email profile',
})

function rowsFromExtraParams(obj: Record<string, string> | null | undefined): { key: string; value: string }[] {
  if (!obj) return []
  return Object.entries(obj).map(([key, value]) => ({ key, value }))
}
function rowsToExtraParams(rows: { key: string; value: string }[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const row of rows) {
    const k = row.key.trim()
    if (k) out[k] = row.value
  }
  return out
}

async function fetchOIDC() {
  try {
    const res = await api.get('/api/v2/admin/oidc/providers')
    const providers = res.data.providers || []
    providers.forEach((p: any) => {
      p._extraParamsRows = rowsFromExtraParams(p.extra_authorize_params)
    })
    oidcProviders.value = providers
  } catch {
    // ignore
  }
}

async function fetchGroups() {
  try {
    const res = await api.get('/api/v2/admin/groups')
    adminGroups.value = (res.data.groups || []).map((g: any) => ({ id: g.id, name: g.name }))
  } catch {
    // ignore — auto-provision dropdown will fall back to "None" only.
  }
}

async function saveProviderAutoProvision(p: any) {
  savingProviderID.value = p.id
  try {
    await Promise.all([
      api.put(`/api/v2/admin/oidc/providers/${p.id}/auto-provision`, {
        auto_provision: !!p.auto_provision,
        default_group_id: p.default_group_id || 0,
      }),
      api.put(`/api/v2/admin/oidc/providers/${p.id}/require-email-verified`, {
        require_email_verified: !!p.require_email_verified,
      }),
    ])
    toast.add({ severity: 'success', summary: 'Saved', detail: `${p.display_name} provisioning updated`, life: 3000 })
  } catch (e: any) {
    toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to save', life: 5000 })
    await fetchOIDC()
  } finally {
    savingProviderID.value = null
  }
}

async function saveExtraParams(p: any) {
  savingExtraParamsID.value = p.id
  try {
    await api.put(`/api/v2/admin/oidc/providers/${p.id}/extra-params`, {
      extra_authorize_params: rowsToExtraParams(p._extraParamsRows),
    })
    toast.add({ severity: 'success', summary: 'Saved', detail: `${p.display_name} params updated`, life: 3000 })
  } catch (e: any) {
    toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to save', life: 5000 })
    await fetchOIDC()
  } finally {
    savingExtraParamsID.value = null
  }
}

function resetCreateDraft() {
  createDraft.name = ''
  createDraft.display_name = ''
  createDraft.issuer_url = ''
  createDraft.client_id = ''
  createDraft.client_secret = ''
  createDraft.scopes = 'openid email profile'
}

function openCreate() {
  resetCreateDraft()
  showCreate.value = true
}

async function submitCreate() {
  // Backend requires name + issuer_url + client_id + client_secret; display_name
  // and scopes have sane defaults applied server-side but we surface them too.
  if (!createDraft.name || !createDraft.issuer_url || !createDraft.client_id || !createDraft.client_secret) {
    toast.add({ severity: 'warn', summary: 'Missing fields', detail: 'Name, issuer URL, client ID and client secret are required', life: 4000 })
    return
  }
  creating.value = true
  try {
    await api.post('/api/v2/admin/oidc/providers', { ...createDraft })
    toast.add({ severity: 'success', summary: 'Created', detail: `${createDraft.display_name || createDraft.name} added`, life: 3000 })
    showCreate.value = false
    await fetchOIDC()
  } catch (e: any) {
    // Backend includes useful detail for discovery failures, name collisions, etc.
    toast.add({ severity: 'error', summary: 'Failed to create', detail: e.response?.data?.error || 'Unknown error', life: 6000 })
  } finally {
    creating.value = false
  }
}

async function deleteProvider(p: any) {
  if (!confirm(`Delete provider "${p.display_name}"? Existing linked identities will also be removed; users will lose SSO access via this provider.`)) {
    return
  }
  try {
    await api.delete(`/api/v2/admin/oidc/providers/${p.id}`)
    toast.add({ severity: 'success', summary: 'Deleted', detail: `${p.display_name} removed`, life: 3000 })
    await fetchOIDC()
  } catch (e: any) {
    toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to delete', life: 5000 })
  }
}

onMounted(async () => {
  await Promise.all([fetchOIDC(), fetchGroups()])
})
</script>

<template>
  <div class="p-6">
    <div class="flex items-center justify-between mb-6">
      <div>
        <h1 class="text-2xl font-bold">SSO / OIDC</h1>
        <p class="text-sm text-surface-400 mt-1">Identity providers for single sign-on. Users authenticate at their IdP and land in indelible via the OIDC code flow.</p>
      </div>
      <div class="flex items-center gap-3">
        <a href="https://github.com/WithAutonomi/indelible/blob/master/docs/guides/sso.md" target="_blank" rel="noopener"
          class="inline-flex items-center gap-1 text-sm text-primary hover:underline">
          <i class="pi pi-book"></i> Setup guide
        </a>
        <Button label="Add provider" icon="pi pi-plus" @click="openCreate" />
      </div>
    </div>

    <div v-if="oidcProviders.length === 0" class="py-12 text-center text-sm text-surface-400 border border-dashed border-surface-200 rounded-lg">
      No OIDC providers configured.<br />
      <span class="text-xs">Click <strong>Add provider</strong> to wire up Okta, Azure AD, Google Workspace, or any OIDC-compliant IdP.</span>
    </div>

    <div v-else class="divide-y divide-surface-100">
      <div v-for="p in oidcProviders" :key="p.id" class="py-5">
        <div class="flex items-center justify-between mb-4">
          <div class="flex items-center gap-3">
            <p class="text-sm font-medium">{{ p.display_name }}</p>
            <Tag v-if="!p.is_enabled" value="Disabled" severity="warning" />
          </div>
          <Button
            icon="pi pi-trash"
            severity="danger"
            text
            rounded
            aria-label="Delete provider"
            @click="deleteProvider(p)"
          />
        </div>
        <p class="text-xs text-surface-400 -mt-3 mb-4">{{ p.issuer_url }}</p>

        <div class="grid grid-cols-3 gap-6 py-3 pl-4">
          <div>
            <label class="text-sm font-medium">Auto-provision</label>
            <p class="text-xs text-surface-400 mt-1">
              When on, an unknown sub or email pair signs in by creating a new local user.
              When off, the same login is rejected with <code>no_account</code>.
              <span class="block mt-1 text-orange-500">
                Email-collision is always blocked — never auto-link by email alone.
              </span>
            </p>
          </div>
          <div class="col-span-2 flex items-center gap-3">
            <ToggleSwitch v-model="p.auto_provision" />
            <span class="text-sm text-surface-500">{{ p.auto_provision ? 'Enabled' : 'Disabled' }}</span>
          </div>
        </div>

        <div class="grid grid-cols-3 gap-6 py-3 pl-4">
          <div>
            <label class="text-sm font-medium">Default group</label>
            <p class="text-xs text-surface-400 mt-1">Auto-provisioned users join this group. Leave at <em>None</em> for no group assignment.</p>
          </div>
          <div class="col-span-2">
            <Select
              v-model="p.default_group_id"
              :options="[{ id: 0, name: 'None' }, ...adminGroups]"
              optionLabel="name"
              optionValue="id"
              class="w-full max-w-md"
            />
          </div>
        </div>

        <div class="grid grid-cols-3 gap-6 py-3 pl-4">
          <div>
            <label class="text-sm font-medium">Require verified email</label>
            <p class="text-xs text-surface-400 mt-1">
              When on, auto-provisioning requires <code>email_verified: true</code> in the IdP's id_token.
              Turn off only for IdPs that don't emit the claim — Okta integrator tenants, Azure AD, and generic OIDC providers per §5.1 of OIDC Core (where the claim is optional).
              <span class="block mt-1 text-orange-500">
                Email-collision is always blocked even with this off.
              </span>
            </p>
          </div>
          <div class="col-span-2 flex items-center gap-3">
            <ToggleSwitch v-model="p.require_email_verified" />
            <span class="text-sm text-surface-500">{{ p.require_email_verified ? 'Required' : 'Not required' }}</span>
          </div>
        </div>

        <div class="flex justify-end mt-3">
          <Button
            :label="savingProviderID === p.id ? 'Saving...' : 'Save provisioning'"
            :loading="savingProviderID === p.id"
            size="small"
            @click="saveProviderAutoProvision(p)"
          />
        </div>

        <div class="grid grid-cols-3 gap-6 py-3 pl-4 border-t border-surface-100 mt-3">
          <div>
            <label class="text-sm font-medium">Extra authorize params</label>
            <p class="text-xs text-surface-400 mt-1">
              Extra query parameters appended to the IdP authorize URL. Most providers need none.
              <span class="block mt-1">
                <strong>Google Workspace:</strong> set <code>hd</code> = <code>your-domain.com</code> to restrict signin to your tenant
                (without this, an "External" OAuth client accepts any personal <code>@gmail.com</code> account).
              </span>
              <span class="block mt-1"><strong>Microsoft / AAD:</strong> e.g. <code>prompt</code> = <code>select_account</code>, <code>domain_hint</code> = <code>your-domain.com</code>.</span>
            </p>
          </div>
          <div class="col-span-2 space-y-2">
            <div v-for="(row, i) in p._extraParamsRows" :key="i" class="flex gap-2 items-center">
              <InputText v-model="row.key" placeholder="key (e.g. hd)" class="flex-1 max-w-xs" />
              <InputText v-model="row.value" placeholder="value (e.g. company.com)" class="flex-1 max-w-xs" />
              <Button
                icon="pi pi-times"
                severity="secondary"
                outlined
                size="small"
                aria-label="Remove param"
                @click="p._extraParamsRows.splice(i, 1)"
              />
            </div>
            <Button
              label="Add param"
              icon="pi pi-plus"
              severity="secondary"
              outlined
              size="small"
              @click="p._extraParamsRows.push({ key: '', value: '' })"
            />
          </div>
        </div>

        <div class="flex justify-end mt-3">
          <Button
            :label="savingExtraParamsID === p.id ? 'Saving...' : 'Save params'"
            :loading="savingExtraParamsID === p.id"
            size="small"
            severity="secondary"
            outlined
            @click="saveExtraParams(p)"
          />
        </div>
      </div>
    </div>

    <Dialog
      v-model:visible="showCreate"
      modal
      header="Add OIDC provider"
      :style="{ width: '36rem' }"
      :closable="!creating"
    >
      <div class="space-y-4 py-2">
        <div>
          <label class="text-sm font-medium">Name (slug)</label>
          <InputText v-model="createDraft.name" placeholder="okta" class="w-full mt-1" />
          <p class="text-xs text-surface-400 mt-1">Lowercase identifier, must be unique. Used internally.</p>
        </div>
        <div>
          <label class="text-sm font-medium">Display name</label>
          <InputText v-model="createDraft.display_name" placeholder="Okta" class="w-full mt-1" />
          <p class="text-xs text-surface-400 mt-1">Shown on the login page. Defaults to the slug if blank.</p>
        </div>
        <div>
          <label class="text-sm font-medium">Issuer URL</label>
          <InputText v-model="createDraft.issuer_url" placeholder="https://tenant.okta.com" class="w-full mt-1" />
          <p class="text-xs text-surface-400 mt-1">Base URL where indelible can discover <code>/.well-known/openid-configuration</code>.</p>
        </div>
        <div>
          <label class="text-sm font-medium">Client ID</label>
          <InputText v-model="createDraft.client_id" class="w-full mt-1" />
        </div>
        <div>
          <label class="text-sm font-medium">Client secret</label>
          <Password v-model="createDraft.client_secret" :feedback="false" toggleMask class="w-full mt-1" inputClass="w-full" />
          <p class="text-xs text-surface-400 mt-1">Stored encrypted at rest. Cannot be retrieved later — keep a copy in your IdP if you need to rotate.</p>
        </div>
        <div>
          <label class="text-sm font-medium">Scopes</label>
          <InputText v-model="createDraft.scopes" class="w-full mt-1" />
          <p class="text-xs text-surface-400 mt-1">Space- or comma-separated. <code>openid email profile</code> covers the standard auto-provision claims.</p>
        </div>
      </div>
      <template #footer>
        <Button label="Cancel" severity="secondary" outlined :disabled="creating" @click="showCreate = false" />
        <Button :label="creating ? 'Creating...' : 'Create provider'" :loading="creating" @click="submitCreate" />
      </template>
    </Dialog>
  </div>
</template>
