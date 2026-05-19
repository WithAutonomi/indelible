<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '../../api/client'
import { useAuthStore } from '../../stores/auth'
import InputText from 'primevue/inputtext'
import Password from 'primevue/password'
import Button from 'primevue/button'
import Message from 'primevue/message'

const auth = useAuthStore()
const router = useRouter()
const route = useRoute()

const email = ref('')
const password = ref('')
const error = ref('')
const loading = ref(false)

// SSO providers exposed via GET /api/v2/auth/oidc/providers — shaped {id, name, display_name}.
type SSOProvider = { id: number; name: string; display_name: string }
const ssoProviders = ref<SSOProvider[]>([])

// Human-readable copy for the ?error=... codes the OIDC callback can hand back.
const ssoErrorMessages: Record<string, string> = {
  no_account:
    'No account is linked to that identity. Ask an admin to enable auto-provisioning or to invite you first.',
  email_exists:
    'An account with that email already exists with a different login method. Ask an admin to link it manually.',
  email_taken:
    'An account with that email already exists with a different login method. Ask an admin to link it manually.',
  session_expired:
    'Your sign-in session expired or was tampered with. Please try again.',
  missing_email:
    'The identity provider did not return a verified email address. Ask your provider admin to expose it.',
  provider_disabled:
    'That identity provider is disabled. Ask an admin to re-enable it.',
  access_denied:
    'Sign-in was cancelled at the identity provider.',
  internal:
    'Sign-in failed unexpectedly. Try again, or use email + password.',
}

onMounted(async () => {
  // Show any error the OIDC callback redirected us back with.
  const code = (route.query.error as string) || ''
  if (code) {
    error.value = ssoErrorMessages[code] || `Sign-in failed (${code}). Try again.`
  }

  try {
    const res = await api.get('/api/v2/auth/oidc/providers')
    ssoProviders.value = res.data.providers || []
  } catch {
    // Endpoint may be off in some deployments — leave the list empty.
  }
})

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

function startSSO(providerId: number) {
  // Full page navigation: the authorize endpoint sets a state cookie and
  // 302's straight to the IdP. fetch() would strand us with no cookie.
  window.location.href = `/api/v2/auth/oidc/authorize/${providerId}`
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

      <!-- SSO providers: only render once we've confirmed at least one is enabled. -->
      <div v-if="ssoProviders.length > 0" class="space-y-3">
        <div class="flex items-center gap-3 text-xs text-surface-400">
          <div class="h-px flex-1 bg-surface-200"></div>
          <span>or</span>
          <div class="h-px flex-1 bg-surface-200"></div>
        </div>
        <Button
          v-for="p in ssoProviders"
          :key="p.id"
          :label="`Sign in with ${p.display_name}`"
          severity="secondary"
          outlined
          class="w-full"
          @click="startSSO(p.id)"
        />
      </div>

      <p class="text-center text-sm text-surface-500">
        No account? <router-link to="/register" class="text-primary font-medium hover:underline">Register</router-link>
        <br />
        <router-link to="/forgot-password" class="text-primary font-medium hover:underline">Forgot password?</router-link>
      </p>
    </div>
  </div>
</template>
