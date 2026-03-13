<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../../api/client'

const route = useRoute()
const router = useRouter()

const newPassword = ref('')
const confirmPassword = ref('')
const loading = ref(false)
const error = ref('')
const success = ref(false)

const token = route.query.token as string

async function handleReset() {
  error.value = ''
  if (newPassword.value.length < 8) {
    error.value = 'Password must be at least 8 characters.'
    return
  }
  if (newPassword.value !== confirmPassword.value) {
    error.value = 'Passwords do not match.'
    return
  }

  loading.value = true
  try {
    await api.post('/api/v2/auth/reset-password', {
      token,
      new_password: newPassword.value,
    })
    success.value = true
    setTimeout(() => router.push('/login'), 3000)
  } catch (e: any) {
    error.value = e.response?.data?.error || 'Reset failed'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="flex min-h-screen items-center justify-center bg-gray-50">
    <div class="w-full max-w-md space-y-6 rounded-lg bg-white p-8 shadow">
      <h1 class="text-2xl font-bold text-center">Reset Password</h1>

      <div v-if="!token" class="rounded bg-red-50 p-4 text-red-700 text-sm text-center">
        Invalid reset link. Please request a new one.
      </div>

      <div v-else-if="success" class="rounded bg-green-50 p-4 text-green-700 text-sm text-center">
        Password reset successfully! Redirecting to login...
      </div>

      <template v-else>
        <div v-if="error" class="rounded bg-red-50 p-3 text-red-700 text-sm">{{ error }}</div>

        <form @submit.prevent="handleReset" class="space-y-4">
          <div>
            <label class="block text-sm font-medium text-gray-700">New Password</label>
            <input v-model="newPassword" type="password" required minlength="8"
              class="mt-1 block w-full rounded border border-gray-300 px-3 py-2" />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700">Confirm Password</label>
            <input v-model="confirmPassword" type="password" required
              class="mt-1 block w-full rounded border border-gray-300 px-3 py-2" />
          </div>
          <button type="submit" :disabled="loading"
            class="w-full rounded bg-blue-600 px-4 py-2 text-white hover:bg-blue-700 disabled:opacity-50">
            {{ loading ? 'Resetting...' : 'Reset Password' }}
          </button>
        </form>
      </template>

      <p class="text-center text-sm text-gray-500">
        <router-link to="/login" class="text-blue-600 hover:underline">Back to login</router-link>
      </p>
    </div>
  </div>
</template>
