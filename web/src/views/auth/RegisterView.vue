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
const firstName = ref('')
const lastName = ref('')
const error = ref('')
const loading = ref(false)

async function handleRegister() {
  loading.value = true
  error.value = ''
  try {
    await auth.register(email.value, password.value, firstName.value, lastName.value)
    router.push('/')
  } catch (e: any) {
    error.value = e.response?.data?.error || 'Registration failed'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="flex min-h-screen items-center justify-center bg-surface-50">
    <div class="w-full max-w-md space-y-6 rounded-xl bg-surface-0 p-8 shadow-md">
      <img src="/favicon.svg" alt="Indelible" width="48" height="48" class="mx-auto" />
      <h1 class="text-2xl font-bold text-center text-surface-900">Indelible</h1>
      <p class="text-center text-surface-500">Create your account</p>

      <Message v-if="error" severity="error" :closable="false">{{ error }}</Message>

      <form @submit.prevent="handleRegister" class="space-y-4">
        <div class="grid grid-cols-2 gap-4">
          <div class="flex flex-col gap-1">
            <label class="text-sm font-medium text-surface-700">First name</label>
            <InputText v-model="firstName" type="text" placeholder="First name" class="w-full" required />
          </div>
          <div class="flex flex-col gap-1">
            <label class="text-sm font-medium text-surface-700">Last name</label>
            <InputText v-model="lastName" type="text" placeholder="Last name" class="w-full" required />
          </div>
        </div>
        <div class="flex flex-col gap-1">
          <label class="text-sm font-medium text-surface-700">Email</label>
          <InputText v-model="email" type="email" placeholder="Email" class="w-full" required />
        </div>
        <div class="flex flex-col gap-1">
          <label class="text-sm font-medium text-surface-700">Password</label>
          <Password v-model="password" toggleMask placeholder="Password" class="w-full" :feedback="false" inputClass="w-full" required />
        </div>
        <Button label="Create account" type="submit" :loading="loading" class="w-full" />
      </form>

      <p class="text-center text-sm text-surface-500">
        Already have an account? <router-link to="/login" class="text-primary font-medium hover:underline">Sign in</router-link>
      </p>
    </div>
  </div>
</template>
