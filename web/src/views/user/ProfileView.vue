<script setup lang="ts">
import { ref, reactive, watch, onMounted, computed } from 'vue'
import { useToast } from 'primevue/usetoast'
import { api } from '../../api/client'
import { useAuthStore } from '../../stores/auth'
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import Password from 'primevue/password'
import Card from 'primevue/card'
import Message from 'primevue/message'

const auth = useAuthStore()
const toast = useToast()

// Connected accounts (OIDC identities + available providers to link).
type OIDCIdentity = { id: number; provider_id: number; provider_name: string; subject: string; created_at: string }
type SSOProvider = { id: number; name: string; display_name: string }
const identities = ref<OIDCIdentity[]>([])
const providers = ref<SSOProvider[]>([])

// Profile card - dirty tracking
const profileSaved = ref({ firstName: '', lastName: '' })
const profile = reactive({ firstName: '', lastName: '' })
const profileDirty = ref(false)
const saving = ref(false)
const profileMsg = ref('')

watch(profile, () => {
  profileDirty.value =
    profile.firstName !== profileSaved.value.firstName ||
    profile.lastName !== profileSaved.value.lastName
}, { deep: true })

// Password card
const currentPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const changingPassword = ref(false)
const passwordMsg = ref('')
const passwordError = ref('')

const passwordDirty = ref(false)
watch([currentPassword, newPassword, confirmPassword], () => {
  passwordDirty.value = !!(currentPassword.value || newPassword.value || confirmPassword.value)
})

onMounted(async () => {
  if (auth.user) {
    profile.firstName = auth.user.first_name
    profile.lastName = auth.user.last_name
    profileSaved.value = { firstName: auth.user.first_name, lastName: auth.user.last_name }
  }
  await fetchConnectedAccounts()
})

async function fetchConnectedAccounts() {
  try {
    const [idsRes, provRes] = await Promise.all([
      api.get('/api/v2/me/oidc/identities'),
      api.get('/api/v2/auth/oidc/providers'),
    ])
    identities.value = idsRes.data.identities || []
    providers.value = provRes.data.providers || []
  } catch {
    // Endpoint may be off; leave the section empty.
  }
}

async function linkProvider(p: SSOProvider) {
  try {
    const res = await api.post(`/api/v2/auth/oidc/link/${p.id}`)
    const url = res.data.authorize_url as string
    if (url) {
      window.location.href = url
    }
  } catch (e: any) {
    toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to start link flow', life: 5000 })
  }
}

async function unlinkIdentity(id: OIDCIdentity) {
  if (!confirm(`Unlink ${id.provider_name}?`)) return
  try {
    await api.delete(`/api/v2/auth/oidc/identities/${id.id}`)
    await fetchConnectedAccounts()
    toast.add({ severity: 'success', summary: 'Unlinked', detail: `${id.provider_name} removed`, life: 3000 })
  } catch (e: any) {
    const code = e.response?.data?.code
    if (code === 'last_login_method') {
      toast.add({
        severity: 'warn',
        summary: 'Cannot unlink',
        detail: 'This is your only login method. Set a password first or link another provider.',
        life: 7000,
      })
      return
    }
    toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to unlink', life: 5000 })
  }
}

// Providers the user hasn't yet linked — drives the "Link another" buttons.
const linkableProviders = computed(() =>
  providers.value.filter(p => !identities.value.some(id => id.provider_id === p.id))
)

async function updateProfile() {
  saving.value = true
  profileMsg.value = ''
  try {
    await api.put('/api/v2/me', {
      first_name: profile.firstName,
      last_name: profile.lastName,
    })
    await auth.fetchProfile()
    profileSaved.value = { ...profile }
    profileDirty.value = false
    profileMsg.value = 'Profile updated.'
    setTimeout(() => profileMsg.value = '', 3000)
  } catch (e: any) {
    toast.add({ severity: 'error', summary: 'Error', detail: e.response?.data?.error || 'Failed to update profile', life: 5000 })
  } finally {
    saving.value = false
  }
}

function discardProfile() {
  Object.assign(profile, profileSaved.value)
}

async function changePassword() {
  passwordError.value = ''
  passwordMsg.value = ''

  if (newPassword.value.length < 8) {
    passwordError.value = 'New password must be at least 8 characters.'
    return
  }
  if (newPassword.value !== confirmPassword.value) {
    passwordError.value = 'Passwords do not match.'
    return
  }

  changingPassword.value = true
  try {
    await api.put('/api/v2/me/password', {
      current_password: currentPassword.value,
      new_password: newPassword.value,
    })
    passwordMsg.value = 'Password changed successfully.'
    discardPassword()
    setTimeout(() => passwordMsg.value = '', 3000)
  } catch (e: any) {
    passwordError.value = e.response?.data?.error || 'Failed to change password'
  } finally {
    changingPassword.value = false
  }
}

function discardPassword() {
  currentPassword.value = ''
  newPassword.value = ''
  confirmPassword.value = ''
}
</script>

<template>
  <div class="p-6">
    <h1 class="text-2xl font-bold mb-6">Profile</h1>

    <!-- Profile info -->
    <Card class="mb-6">
      <template #title>Personal Information</template>
      <template #content>
        <Message v-if="profileMsg" severity="success" :closable="false" class="mb-4">{{ profileMsg }}</Message>

        <div class="flex flex-col gap-5">
          <div class="grid grid-cols-3 gap-6">
            <div>
              <label class="text-sm font-medium text-gray-700">Email</label>
              <p class="text-xs text-gray-400 mt-1">Your login email cannot be changed.</p>
            </div>
            <div class="col-span-2">
              <InputText :modelValue="auth.user?.email" disabled class="w-full max-w-md" />
            </div>
          </div>
          <div class="grid grid-cols-3 gap-6">
            <div>
              <label class="text-sm font-medium text-gray-700">First Name</label>
            </div>
            <div class="col-span-2">
              <InputText v-model="profile.firstName" required class="w-full max-w-md" />
            </div>
          </div>
          <div class="grid grid-cols-3 gap-6">
            <div>
              <label class="text-sm font-medium text-gray-700">Last Name</label>
            </div>
            <div class="col-span-2">
              <InputText v-model="profile.lastName" required class="w-full max-w-md" />
            </div>
          </div>
        </div>

        <div v-if="profileDirty" class="flex items-center justify-between mt-6 pt-4 border-t border-gray-200">
          <p class="text-xs text-gray-500">You have unsaved changes</p>
          <div class="flex gap-2">
            <Button label="Discard" severity="secondary" text @click="discardProfile" />
            <Button :label="saving ? 'Saving...' : 'Save'" :loading="saving" @click="updateProfile" />
          </div>
        </div>
      </template>
    </Card>

    <!-- Connected accounts (OIDC identities) -->
    <Card v-if="identities.length > 0 || linkableProviders.length > 0" class="mb-6">
      <template #title>Connected Accounts</template>
      <template #content>
        <p class="text-sm text-surface-400 mb-4">
          Identity providers you can sign in with. Linking a provider lets you sign in via your company SSO in addition to email + password.
        </p>

        <div v-if="identities.length > 0" class="divide-y divide-surface-100">
          <div v-for="id in identities" :key="id.id" class="flex items-center justify-between py-3">
            <div>
              <p class="text-sm font-medium">{{ id.provider_name }}</p>
              <p class="text-xs text-surface-400">Subject: <code>{{ id.subject }}</code> &middot; linked {{ id.created_at }}</p>
            </div>
            <Button label="Unlink" severity="danger" outlined size="small" @click="unlinkIdentity(id)" />
          </div>
        </div>

        <div v-if="linkableProviders.length > 0" class="mt-4 pt-4 border-t border-surface-100">
          <p class="text-xs text-surface-400 mb-3">Link another provider:</p>
          <div class="flex flex-wrap gap-2">
            <Button
              v-for="p in linkableProviders"
              :key="p.id"
              :label="`Link ${p.display_name}`"
              severity="secondary"
              outlined
              size="small"
              @click="linkProvider(p)"
            />
          </div>
        </div>
      </template>
    </Card>

    <!-- Change password -->
    <Card>
      <template #title>Change Password</template>
      <template #content>
        <Message v-if="passwordMsg" severity="success" :closable="false" class="mb-4">{{ passwordMsg }}</Message>
        <Message v-if="passwordError" severity="error" :closable="false" class="mb-4">{{ passwordError }}</Message>

        <div class="flex flex-col gap-5">
          <div class="grid grid-cols-3 gap-6">
            <div>
              <label class="text-sm font-medium text-gray-700">Current Password</label>
            </div>
            <div class="col-span-2">
              <Password v-model="currentPassword" :feedback="false" toggleMask inputClass="w-full" class="w-full max-w-md" />
            </div>
          </div>
          <div class="grid grid-cols-3 gap-6">
            <div>
              <label class="text-sm font-medium text-gray-700">New Password</label>
              <p class="text-xs text-gray-400 mt-1">Minimum 8 characters.</p>
            </div>
            <div class="col-span-2">
              <Password v-model="newPassword" toggleMask inputClass="w-full" class="w-full max-w-md" />
            </div>
          </div>
          <div class="grid grid-cols-3 gap-6">
            <div>
              <label class="text-sm font-medium text-gray-700">Confirm Password</label>
            </div>
            <div class="col-span-2">
              <Password v-model="confirmPassword" :feedback="false" toggleMask inputClass="w-full" class="w-full max-w-md" />
            </div>
          </div>
        </div>

        <div v-if="passwordDirty" class="flex items-center justify-between mt-6 pt-4 border-t border-gray-200">
          <p class="text-xs text-gray-500">You have unsaved changes</p>
          <div class="flex gap-2">
            <Button label="Discard" severity="secondary" text @click="discardPassword" />
            <Button :label="changingPassword ? 'Changing...' : 'Change Password'"
              :loading="changingPassword" @click="changePassword" />
          </div>
        </div>
      </template>
    </Card>
  </div>
</template>
