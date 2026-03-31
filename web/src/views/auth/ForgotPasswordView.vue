<script setup lang="ts">
import { ref } from 'vue'
import { api } from '../../api/client'
import InputText from 'primevue/inputtext'
import Button from 'primevue/button'
import Message from 'primevue/message'

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
  <div class="flex min-h-screen items-center justify-center bg-surface-50">
    <div class="w-full max-w-md space-y-6 rounded-xl bg-surface-0 p-8 shadow-md">
      <h1 class="text-2xl font-bold text-center text-surface-900">Forgot Password</h1>

      <Message v-if="sent" severity="success" :closable="false">
        If that email exists, a reset link has been sent. Check your inbox.
      </Message>

      <template v-else>
        <p class="text-center text-surface-500 text-sm">
          Enter your email address and we'll send you a password reset link.
        </p>

        <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>

        <form @submit.prevent="handleSubmit" class="space-y-4">
          <div class="flex flex-col gap-1">
            <label class="text-sm font-medium text-surface-700">Email</label>
            <InputText v-model="email" type="email" placeholder="Email" class="w-full" required />
          </div>
          <Button label="Send Reset Link" type="submit" :loading="loading" class="w-full" />
        </form>
      </template>

      <p class="text-center text-sm text-surface-500">
        <router-link to="/login" class="text-primary font-medium hover:underline">Back to login</router-link>
      </p>
    </div>
  </div>
</template>
