<script setup lang="ts">
import { ref } from 'vue'
import { api } from '../../api/client'

const email = ref('')
const sent = ref(false)
const loading = ref(false)
const error = ref('')

async function handleSubmit() {
  loading.value = true
  error.value = ''
  try {
    await api.post('/api/v2/auth/forgot-password', { email: email.value })
    sent.value = true
  } catch (e: any) {
    error.value = e.response?.data?.error || 'Request failed'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="flex min-h-screen items-center justify-center bg-gray-50">
    <div class="w-full max-w-md space-y-6 rounded-lg bg-white p-8 shadow">
      <h1 class="text-2xl font-bold text-center">Forgot Password</h1>

      <div v-if="sent" class="rounded bg-green-50 p-4 text-green-700 text-sm text-center">
        If that email exists, a reset link has been sent. Check your inbox.
      </div>

      <template v-else>
        <p class="text-center text-gray-500 text-sm">
          Enter your email address and we'll send you a password reset link.
        </p>

        <div v-if="error" class="rounded bg-red-50 p-3 text-red-700 text-sm">{{ error }}</div>

        <form @submit.prevent="handleSubmit" class="space-y-4">
          <div>
            <label class="block text-sm font-medium text-gray-700">Email</label>
            <input v-model="email" type="email" required
              class="mt-1 block w-full rounded border border-gray-300 px-3 py-2" />
          </div>
          <button type="submit" :disabled="loading"
            class="w-full rounded bg-blue-600 px-4 py-2 text-white hover:bg-blue-700 disabled:opacity-50">
            {{ loading ? 'Sending...' : 'Send Reset Link' }}
          </button>
        </form>
      </template>

      <p class="text-center text-sm text-gray-500">
        <router-link to="/login" class="text-blue-600 hover:underline">Back to login</router-link>
      </p>
    </div>
  </div>
</template>
