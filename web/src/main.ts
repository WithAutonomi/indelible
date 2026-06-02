import { createApp } from 'vue'
import { createPinia } from 'pinia'
import PrimeVue from 'primevue/config'
import ConfirmationService from 'primevue/confirmationservice'
import ToastService from 'primevue/toastservice'
import Tooltip from 'primevue/tooltip'
import { definePreset } from '@primevue/themes'
import Aura from '@primevue/themes/aura'
import 'primeicons/primeicons.css'

import App from './App.vue'
import router from './router'
import { useAuthStore } from './stores/auth'
import { initTheme } from './composables/useTheme'

import './assets/main.css'

// Deep-navy dark theme. The light scheme keeps Aura's slate surfaces; only the
// dark surface ramp is overridden — Aura's default dark surface is neutral zinc
// (near-black), this makes it a deep navy (not pure black) per the brand.
const Navy = definePreset(Aura, {
  semantic: {
    colorScheme: {
      dark: {
        surface: {
          0: '#ffffff',
          50: '#f4f6fb',
          100: '#e4e9f4',
          200: '#c2cce3',
          300: '#94a3c9',
          400: '#6675a3',
          500: '#465379',
          600: '#34405f',
          700: '#27314c',
          800: '#1b2440',
          900: '#141d36',
          950: '#0c1428',
        },
      },
    },
  },
})

// Apply the saved/system theme before mount to avoid a flash of the wrong mode.
initTheme()

const app = createApp(App)

app.use(createPinia())
app.use(PrimeVue, {
  theme: {
    preset: Navy,
    options: {
      darkModeSelector: '.app-dark',  // dark mode toggled by the .app-dark class (see useTheme)
    },
  },
})
app.use(ConfirmationService)
app.use(ToastService)
// Register the v-tooltip directive — without this every `v-tooltip` in the app
// is a silent no-op (action-button hovers, the public-file badge, etc.).
app.directive('tooltip', Tooltip)

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
