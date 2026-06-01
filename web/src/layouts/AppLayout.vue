<script setup lang="ts">
import { computed, watch, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import { useHealthStore } from '../stores/health'
import Button from 'primevue/button'
import Avatar from 'primevue/avatar'

const auth = useAuthStore()
const router = useRouter()
const health = useHealthStore()

// Poll antd/network health only while an admin is signed in — the status
// banner and System view are admin-only, and /health is a cheap unauthenticated
// probe. immediate so it kicks off as soon as the (already-authenticated)
// layout mounts; the watcher also handles a role change within the session.
watch(
  () => auth.isAdmin,
  (isAdmin) => (isAdmin ? health.start() : health.stop()),
  { immediate: true },
)
onUnmounted(() => health.stop())

const userName = computed(() => {
  if (!auth.user) return ''
  return `${auth.user.first_name} ${auth.user.last_name}`
})

const initials = computed(() => {
  if (!auth.user) return ''
  return `${auth.user.first_name?.[0] ?? ''}${auth.user.last_name?.[0] ?? ''}`
})

async function logout() {
  await auth.logout()
  router.push('/login')
}

const navItems = computed(() => {
  const items = [
    { label: 'Dashboard', icon: 'pi pi-home', to: '/' },
    { label: 'Uploads', icon: 'pi pi-upload', to: '/uploads' },
    { label: 'Collections', icon: 'pi pi-folder', to: '/collections' },
    { label: 'API Tokens', icon: 'pi pi-key', to: '/tokens' },
  ]
  if (auth.isAdmin) {
    items.push(
      { label: 'Users', icon: 'pi pi-users', to: '/admin/users' },
      { label: 'Groups', icon: 'pi pi-id-card', to: '/admin/groups' },
      { label: 'Wallets', icon: 'pi pi-wallet', to: '/admin/wallets' },
      { label: 'Quotas', icon: 'pi pi-gauge', to: '/admin/quotas' },
      { label: 'Tag Rules', icon: 'pi pi-tags', to: '/admin/tag-rules' },
      { label: 'Webhooks', icon: 'pi pi-bell', to: '/admin/webhooks' },
      { label: 'SSO', icon: 'pi pi-sign-in', to: '/admin/sso' },
      { label: 'SCIM', icon: 'pi pi-sync', to: '/admin/scim' },
      { label: 'Settings', icon: 'pi pi-cog', to: '/admin/settings' },
      { label: 'Analytics', icon: 'pi pi-chart-bar', to: '/admin/analytics' },
      { label: 'Logs', icon: 'pi pi-list', to: '/admin/logs' },
      { label: 'System', icon: 'pi pi-server', to: '/admin/system' },
    )
  }
  return items
})
</script>

<template>
  <div class="flex min-h-screen bg-surface-50">
    <!-- Sidebar -->
    <aside class="w-60 bg-surface-0 border-r border-surface-200 flex flex-col">
      <div class="p-4 border-b border-surface-200">
        <h1 class="text-xl font-bold text-surface-800">Indelible</h1>
        <p class="text-xs text-surface-400 mt-1">Autonomi Storage Gateway</p>
      </div>

      <nav class="flex-1 p-3 space-y-0.5">
        <router-link
          v-for="item in navItems"
          :key="item.to"
          :to="item.to"
          class="flex items-center gap-3 px-3 py-2 rounded-lg text-sm text-surface-600 hover:bg-surface-100 transition-colors"
          active-class="!bg-primary/10 !text-primary font-medium"
        >
          <i :class="item.icon" class="text-base"></i>
          {{ item.label }}
        </router-link>
      </nav>

      <div class="p-3 border-t border-surface-200">
        <div class="flex items-center gap-3 px-3 py-2">
          <Avatar :label="initials" shape="circle" class="bg-primary/10 text-primary" />
          <router-link to="/profile" class="flex-1 min-w-0 hover:opacity-80">
            <p class="text-sm font-medium text-surface-700 truncate">{{ userName }}</p>
            <p class="text-xs text-surface-400 truncate">{{ auth.user?.email }}</p>
          </router-link>
          <Button icon="pi pi-sign-out" text rounded severity="secondary" @click="logout" v-tooltip="'Logout'" />
        </div>
      </div>
    </aside>

    <!-- Main content -->
    <main class="flex-1 overflow-auto">
      <!-- Network/daemon health banner (admins only). The reported condition
           is otherwise invisible in the UI — without this it only shows up in
           the container logs. -->
      <div
        v-if="auth.isAdmin && health.networkDegraded"
        class="flex items-center gap-3 px-6 py-3 bg-amber-50 border-b border-amber-200 text-amber-800 text-sm"
        role="alert"
      >
        <i class="pi pi-exclamation-triangle text-amber-500"></i>
        <span class="flex-1">
          <template v-if="!health.serverReachable">
            Can't reach the Indelible backend — the service may be down or restarting.
          </template>
          <template v-else>
            The Autonomi daemon (antd) isn't reaching the network. Uploads and downloads
            will fail until it reconnects.
          </template>
        </span>
        <router-link
          to="/admin/system"
          class="font-medium underline hover:no-underline whitespace-nowrap"
        >
          View status
        </router-link>
      </div>
      <slot />
    </main>
  </div>
</template>
