<script setup lang="ts">
import { ref, reactive, watch, onMounted } from 'vue'
import { api } from '../../api/client'
import { useAuthStore } from '../../stores/auth'

const auth = useAuthStore()

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
    alert(e.response?.data?.error || 'Failed to update profile')
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
    <div class="bg-white rounded-lg border border-gray-200 mb-6">
      <div class="px-6 py-4 border-b border-gray-200">
        <h2 class="text-base font-semibold text-gray-800">Personal Information</h2>
      </div>
      <div v-if="profileMsg" class="mx-6 mt-4 rounded bg-green-50 p-3 text-green-700 text-sm">{{ profileMsg }}</div>
      <div class="divide-y divide-gray-100">
        <div class="grid grid-cols-3 gap-6 px-6 py-5">
          <div>
            <label class="text-sm font-medium text-gray-700">Email</label>
            <p class="text-xs text-gray-400 mt-1">Your login email cannot be changed.</p>
          </div>
          <div class="col-span-2">
            <input :value="auth.user?.email" disabled
              class="block w-full max-w-md rounded border border-gray-200 bg-gray-50 px-3 py-2 text-sm text-gray-500" />
          </div>
        </div>
        <div class="grid grid-cols-3 gap-6 px-6 py-5">
          <div>
            <label class="text-sm font-medium text-gray-700">First Name</label>
          </div>
          <div class="col-span-2">
            <input v-model="profile.firstName" type="text" required
              class="block w-full max-w-md rounded border border-gray-300 px-3 py-2 text-sm" />
          </div>
        </div>
        <div class="grid grid-cols-3 gap-6 px-6 py-5">
          <div>
            <label class="text-sm font-medium text-gray-700">Last Name</label>
          </div>
          <div class="col-span-2">
            <input v-model="profile.lastName" type="text" required
              class="block w-full max-w-md rounded border border-gray-300 px-3 py-2 text-sm" />
          </div>
        </div>
      </div>
      <div v-if="profileDirty" class="px-6 py-4 bg-gray-50 border-t border-gray-200 rounded-b-lg flex items-center justify-between">
        <p class="text-xs text-gray-500">You have unsaved changes</p>
        <div class="flex gap-2">
          <button type="button" @click="discardProfile"
            class="rounded border px-3 py-1.5 text-sm text-gray-600 hover:bg-gray-100">Discard</button>
          <button type="button" @click="updateProfile" :disabled="saving"
            class="rounded bg-blue-600 px-4 py-1.5 text-sm text-white hover:bg-blue-700 disabled:opacity-50">
            {{ saving ? 'Saving...' : 'Save' }}
          </button>
        </div>
      </div>
    </div>

    <!-- Change password -->
    <div class="bg-white rounded-lg border border-gray-200">
      <div class="px-6 py-4 border-b border-gray-200">
        <h2 class="text-base font-semibold text-gray-800">Change Password</h2>
      </div>
      <div v-if="passwordMsg" class="mx-6 mt-4 rounded bg-green-50 p-3 text-green-700 text-sm">{{ passwordMsg }}</div>
      <div v-if="passwordError" class="mx-6 mt-4 rounded bg-red-50 p-3 text-red-700 text-sm">{{ passwordError }}</div>
      <div class="divide-y divide-gray-100">
        <div class="grid grid-cols-3 gap-6 px-6 py-5">
          <div>
            <label class="text-sm font-medium text-gray-700">Current Password</label>
          </div>
          <div class="col-span-2">
            <input v-model="currentPassword" type="password" required
              class="block w-full max-w-md rounded border border-gray-300 px-3 py-2 text-sm" />
          </div>
        </div>
        <div class="grid grid-cols-3 gap-6 px-6 py-5">
          <div>
            <label class="text-sm font-medium text-gray-700">New Password</label>
            <p class="text-xs text-gray-400 mt-1">Minimum 8 characters.</p>
          </div>
          <div class="col-span-2">
            <input v-model="newPassword" type="password" required minlength="8"
              class="block w-full max-w-md rounded border border-gray-300 px-3 py-2 text-sm" />
          </div>
        </div>
        <div class="grid grid-cols-3 gap-6 px-6 py-5">
          <div>
            <label class="text-sm font-medium text-gray-700">Confirm Password</label>
          </div>
          <div class="col-span-2">
            <input v-model="confirmPassword" type="password" required
              class="block w-full max-w-md rounded border border-gray-300 px-3 py-2 text-sm" />
          </div>
        </div>
      </div>
      <div v-if="passwordDirty" class="px-6 py-4 bg-gray-50 border-t border-gray-200 rounded-b-lg flex items-center justify-between">
        <p class="text-xs text-gray-500">You have unsaved changes</p>
        <div class="flex gap-2">
          <button type="button" @click="discardPassword"
            class="rounded border px-3 py-1.5 text-sm text-gray-600 hover:bg-gray-100">Discard</button>
          <button type="button" @click="changePassword" :disabled="changingPassword"
            class="rounded bg-blue-600 px-4 py-1.5 text-sm text-white hover:bg-blue-700 disabled:opacity-50">
            {{ changingPassword ? 'Changing...' : 'Change Password' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
