import { createApp } from 'vue'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ConfirmationService from 'primevue/confirmationservice'
import ToastService from 'primevue/toastservice'
import Aura from '@primevue/themes/aura'
import 'primeicons/primeicons.css'

import App from './App.vue'
import router from './router'
import { useAuthStore } from './stores/auth'

import './assets/main.css'

const app = createApp(App)

app.use(createPinia())
app.use(PrimeVue, {
  theme: {
    preset: Aura,
    options: {
      darkModeSelector: '.app-dark',  // only activate dark mode with explicit class, not system preference
    },
  },
})
app.use(ConfirmationService)
app.use(ToastService)

// Resolve the session BEFORE installing the router so the first navigation
// guard sees an authoritative isAuthenticated. SSO users carry the JWT in an
// HttpOnly cookie that isn't visible to JS — the only way to know they're
// logged in is to ask the server.
async function boot() {
  await useAuthStore().bootstrap()
  app.use(router)
  app.mount('#app')
}

boot()
