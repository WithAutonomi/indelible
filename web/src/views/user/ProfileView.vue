<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api } from '../../api/client'
import { useAuthStore } from '../../stores/auth'

const auth = useAuthStore()

const firstName = ref('')
const lastName = ref('')
const saving = ref(false)
const profileMsg = ref('')

const currentPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const changingPassword = ref(false)
const passwordMsg = ref('')
const passwordError = ref('')

onMounted(() => {
  if (auth.user) {
    firstName.value = auth.user.first_name
    lastName.value = auth.user.last_name
  }
})

async function updateProfile() {
  saving.value = true
  profileMsg.value = ''
  try {
    await api.put('/api/v2/me', {
      first_name: firstName.value,
      last_name: lastName.value,
    })
    await auth.fetchProfile()
    profileMsg.value = 'Profile updated.'
    setTimeout(() => profileMsg.value = '', 3000)
  } catch (e: any) {
    alert(e.response?.data?.error || 'Failed to update profile')
  } finally {
    saving.value = false
  }
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
    currentPassword.value = ''
    newPassword.value = ''
    confirmPassword.value = ''
    setTimeout(() => passwordMsg.value = '', 3000)
  } catch (e: any) {
    passwordError.value = e.response?.data?.error || 'Failed to change password'
  } finally {
    changingPassword.value = false
  }
}
</script>

<template>
  <div class="p-6 max-w-2xl">
    <h1 class="text-2xl font-bold mb-6">Profile</h1>

    <!-- Profile info -->
    <div class="bg-white rounded-lg border border-gray-200 p-6 mb-6">
      <h2 class="text-lg font-semibold mb-4">Personal Information</h2>
      <div v-if="profileMsg" class="mb-3 rounded bg-green-50 p-3 text-green-700 text-sm">{{ profileMsg }}</div>

      <form @submit.prevent="updateProfile" class="space-y-4">
        <div>
          <label class="block text-sm font-medium text-gray-700 mb-1">Email</label>
          <input :value="auth.user?.email" disabled
            class="block w-full rounded border border-gray-200 bg-gray-50 px-3 py-2 text-sm text-gray-500" />
        </div>
        <div class="grid grid-cols-2 gap-4">
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">First Name</label>
            <input v-model="firstName" type="text" required
              class="block w-full rounded border border-gray-300 px-3 py-2 text-sm" />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700 mb-1">Last Name</label>
            <input v-model="lastName" type="text" required
              class="block w-full rounded border border-gray-300 px-3 py-2 text-sm" />
          </div>
        </div>
        <button type="submit" :disabled="saving"
          class="rounded bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700 disabled:opacity-50">
          {{ saving ? 'Saving...' : 'Update Profile' }}
        </button>
      </form>
    </div>

    <!-- Change password -->
    <div class="bg-white rounded-lg border border-gray-200 p-6">
      <h2 class="text-lg font-semibold mb-4">Change Password</h2>
      <div v-if="passwordMsg" class="mb-3 rounded bg-green-50 p-3 text-green-700 text-sm">{{ passwordMsg }}</div>
      <div v-if="passwordError" class="mb-3 rounded bg-red-50 p-3 text-red-700 text-sm">{{ passwordError }}</div>

      <form @submit.prevent="changePassword" class="space-y-4">
        <div>
          <label class="block text-sm font-medium text-gray-700 mb-1">Current Password</label>
          <input v-model="currentPassword" type="password" required
            class="block w-full rounded border border-gray-300 px-3 py-2 text-sm" />
        </div>
        <div>
          <label class="block text-sm font-medium text-gray-700 mb-1">New Password</label>
          <input v-model="newPassword" type="password" required minlength="8"
            class="block w-full rounded border border-gray-300 px-3 py-2 text-sm" />
        </div>
        <div>
          <label class="block text-sm font-medium text-gray-700 mb-1">Confirm New Password</label>
          <input v-model="confirmPassword" type="password" required
            class="block w-full rounded border border-gray-300 px-3 py-2 text-sm" />
        </div>
        <button type="submit" :disabled="changingPassword"
          class="rounded bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-700 disabled:opacity-50">
          {{ changingPassword ? 'Changing...' : 'Change Password' }}
        </button>
      </form>
    </div>
  </div>
</template>
