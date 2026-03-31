<script setup lang="ts">
import { useRoute } from 'vue-router'
import { computed, onMounted } from 'vue'
import { useAuthStore } from './stores/auth'
import AppLayout from './layouts/AppLayout.vue'
import Toast from 'primevue/toast'
import ConfirmDialog from 'primevue/confirmdialog'

const route = useRoute()
const auth = useAuthStore()

const showLayout = computed(() => route.meta.auth && auth.isAuthenticated)

onMounted(async () => {
  if (auth.token && !auth.user) {
    try {
      await auth.fetchProfile()
    } catch {
      auth.logout()
    }
  }
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
