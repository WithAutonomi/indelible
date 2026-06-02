import { ref } from 'vue'

const STORAGE_KEY = 'indelible-theme'
const DARK_CLASS = 'app-dark'

// Module-level so every useTheme() caller shares one reactive source of truth.
const isDark = ref(false)

function applyTheme(dark: boolean) {
  isDark.value = dark
  document.documentElement.classList.toggle(DARK_CLASS, dark)
}

/**
 * Set the initial theme before the app mounts to avoid a flash of the wrong
 * mode: an explicit saved preference wins, otherwise follow the OS
 * prefers-color-scheme. Call once at boot.
 */
export function initTheme() {
  const saved = localStorage.getItem(STORAGE_KEY)
  const prefersDark =
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-color-scheme: dark)').matches
  applyTheme(saved ? saved === 'dark' : prefersDark)
}

export function useTheme() {
  function toggle() {
    const next = !isDark.value
    applyTheme(next)
    localStorage.setItem(STORAGE_KEY, next ? 'dark' : 'light')
  }
  return { isDark, toggle }
}
