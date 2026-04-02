<script setup lang="ts">
import { useRoute } from 'vue-router'
import { computed, onMounted, onUnmounted } from 'vue'
import { useToast } from 'primevue/usetoast'
import { useAuthStore } from './stores/auth'
import AppLayout from './layouts/AppLayout.vue'
import Toast from 'primevue/toast'
import ConfirmDialog from 'primevue/confirmdialog'

const route = useRoute()
const auth = useAuthStore()
const toast = useToast()

const showLayout = computed(() => route.meta.auth && auth.isAuthenticated)

function onSessionExpired() {
  toast.add({ severity: 'error', summary: 'Session Expired', detail: 'Your session has expired. Redirecting to login...', life: 3000 })
}

function onSessionExpiring(e: Event) {
  const minutes = (e as CustomEvent).detail?.minutes || 5
  toast.add({ severity: 'warn', summary: 'Session Expiring', detail: `Your session expires in ${minutes} minute${minutes === 1 ? '' : 's'}. Save your work.`, life: 10000 })
}

onMounted(async () => {
  window.addEventListener('session-expired', onSessionExpired)
  window.addEventListener('session-expiring', onSessionExpiring)

  if (auth.token && !auth.user) {
    try {
      await auth.fetchProfile()
    } catch {
      auth.logout()
    }
  }
})

onUnmounted(() => {
  window.removeEventListener('session-expired', onSessionExpired)
  window.removeEventListener('session-expiring', onSessionExpiring)
})
</script>

<template>
  <Toast position="top-right" />
  <ConfirmDialog />
  <AppLayout v-if="showLayout">
    <RouterView />
  </AppLayout>
  <RouterView v-else />
</template>
