<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../../api/client'
import Password from 'primevue/password'
import Button from 'primevue/button'
import Message from 'primevue/message'

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
  <div class="flex min-h-screen items-center justify-center bg-surface-50">
    <div class="w-full max-w-md space-y-6 rounded-xl bg-surface-0 p-8 shadow-md">
      <h1 class="text-2xl font-bold text-center text-surface-900">Reset Password</h1>

      <Message v-if="!token" severity="error" :closable="false">
        Invalid reset link. Please request a new one.
      </Message>

      <Message v-else-if="success" severity="success" :closable="false">
        Password reset successfully! Redirecting to login...
      </Message>

      <template v-else>
        <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>

        <form @submit.prevent="handleReset" class="space-y-4">
          <div class="flex flex-col gap-1">
            <label class="text-sm font-medium text-surface-700">New Password</label>
            <Password v-model="newPassword" toggleMask placeholder="New password" class="w-full" :feedback="false" inputClass="w-full" required />
          </div>
          <div class="flex flex-col gap-1">
            <label class="text-sm font-medium text-surface-700">Confirm Password</label>
            <Password v-model="confirmPassword" toggleMask placeholder="Confirm password" class="w-full" :feedback="false" inputClass="w-full" required />
          </div>
          <Button label="Reset Password" type="submit" :loading="loading" class="w-full" />
        </form>
      </template>

      <p class="text-center text-sm text-surface-500">
        <router-link to="/login" class="text-primary font-medium hover:underline">Back to login</router-link>
      </p>
    </div>
  </div>
</template>
