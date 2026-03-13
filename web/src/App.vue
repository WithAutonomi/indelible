<script setup lang="ts">
import { useRoute } from 'vue-router'
import { computed, onMounted } from 'vue'
import { useAuthStore } from './stores/auth'
import AppLayout from './layouts/AppLayout.vue'

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
  <AppLayout v-if="showLayout">
    <RouterView />
  </AppLayout>
  <RouterView v-else />
</template>
