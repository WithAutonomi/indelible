<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../../stores/auth'

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
  <div class="flex min-h-screen items-center justify-center bg-gray-50">
    <div class="w-full max-w-md space-y-6 rounded-lg bg-white p-8 shadow">
      <h1 class="text-2xl font-bold text-center">Indelible</h1>
      <p class="text-center text-gray-500">Create your account</p>

      <div v-if="error" class="rounded bg-red-50 p-3 text-red-700 text-sm">{{ error }}</div>

      <form @submit.prevent="handleRegister" class="space-y-4">
        <div class="grid grid-cols-2 gap-4">
          <div>
            <label class="block text-sm font-medium text-gray-700">First name</label>
            <input v-model="firstName" type="text" required
              class="mt-1 block w-full rounded border border-gray-300 px-3 py-2" />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700">Last name</label>
            <input v-model="lastName" type="text" required
              class="mt-1 block w-full rounded border border-gray-300 px-3 py-2" />
          </div>
        </div>
        <div>
          <label class="block text-sm font-medium text-gray-700">Email</label>
          <input v-model="email" type="email" required
            class="mt-1 block w-full rounded border border-gray-300 px-3 py-2" />
        </div>
        <div>
          <label class="block text-sm font-medium text-gray-700">Password</label>
          <input v-model="password" type="password" required minlength="8"
            class="mt-1 block w-full rounded border border-gray-300 px-3 py-2" />
        </div>
        <button type="submit" :disabled="loading"
          class="w-full rounded bg-blue-600 px-4 py-2 text-white hover:bg-blue-700 disabled:opacity-50">
          {{ loading ? 'Creating account...' : 'Create account' }}
        </button>
      </form>

      <p class="text-center text-sm text-gray-500">
        Already have an account? <router-link to="/login" class="text-blue-600 hover:underline">Sign in</router-link>
      </p>
    </div>
  </div>
</template>
