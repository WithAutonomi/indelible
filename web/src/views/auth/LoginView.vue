<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../../stores/auth'
import InputText from 'primevue/inputtext'
import Password from 'primevue/password'
import Button from 'primevue/button'
import Message from 'primevue/message'

const auth = useAuthStore()
const router = useRouter()

const email = ref('')
const password = ref('')
const error = ref('')
const loading = ref(false)

async function handleLogin() {
  loading.value = true
  error.value = ''
  try {
    await auth.login(email.value, password.value)
    router.push('/')
  } catch (e: any) {
    error.value = e.response?.data?.error || 'Login failed'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="flex min-h-screen items-center justify-center bg-surface-50">
    <div class="w-full max-w-md space-y-6 rounded-xl bg-surface-0 p-8 shadow-md">
      <h1 class="text-2xl font-bold text-center text-surface-900">Indelible</h1>
      <p class="text-center text-surface-500">Sign in to your account</p>

      <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>

      <form @submit.prevent="handleLogin" class="space-y-4">
        <div class="flex flex-col gap-1">
          <label class="text-sm font-medium text-surface-700">Email</label>
          <InputText v-model="email" type="email" placeholder="Email" class="w-full" required />
        </div>
        <div class="flex flex-col gap-1">
          <label class="text-sm font-medium text-surface-700">Password</label>
          <Password v-model="password" toggleMask placeholder="Password" class="w-full" :feedback="false" inputClass="w-full" required />
        </div>
        <Button label="Sign in" type="submit" :loading="loading" class="w-full" />
      </form>

      <p class="text-center text-sm text-surface-500">
        No account? <router-link to="/register" class="text-primary font-medium hover:underline">Register</router-link>
        <br />
        <router-link to="/forgot-password" class="text-primary font-medium hover:underline">Forgot password?</router-link>
      </p>
    </div>
  </div>
</template>
