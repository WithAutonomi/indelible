<script setup lang="ts">
import { ref, reactive, watch, onMounted } from 'vue'
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

onMounted(() => {
  if (auth.user) {
    profile.firstName = auth.user.first_name
    profile.lastName = auth.user.last_name
    profileSaved.value = { firstName: auth.user.first_name, lastName: auth.user.last_name }
  }
})

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
