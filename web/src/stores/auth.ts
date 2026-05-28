import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { api } from '../api/client'

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string | null>(localStorage.getItem('token'))
  const user = ref<any>(null)

  // Auth state is the loaded user profile, not the localStorage token: SSO
  // logins put the JWT in an HttpOnly cookie the SPA can't read, so token is
  // null but the user is fully authenticated via cookie auth on every request.
  const isAuthenticated = computed(() => !!user.value)
  const isAdmin = computed(() => user.value?.permissions?.includes('admin') ?? false)

  async function login(email: string, password: string) {
    const res = await api.post('/api/v2/auth/login', { email, password })
    token.value = res.data.token
    localStorage.setItem('token', res.data.token)
    await fetchProfile()
  }

  async function register(email: string, password: string, firstName: string, lastName: string) {
    const res = await api.post('/api/v2/auth/register', {
      email,
      password,
      first_name: firstName,
      last_name: lastName,
    })
    token.value = res.data.token
    localStorage.setItem('token', res.data.token)
    await fetchProfile()
  }

  async function fetchProfile() {
    const res = await api.get('/api/v2/me')
    user.value = res.data
  }

  // Called from main.ts before mount. Tries to resolve the current session
  // from either localStorage (password flow) or the session cookie (SSO flow).
  // Swallows 401: a missing/expired session just means "not logged in" — we
  // must opt out of the client's global 401 redirect-to-/login behavior or
  // it'd loop on every page load before login.
  async function bootstrap() {
    if (user.value) return
    try {
      const res = await api.get('/api/v2/me', { _skipAuthRedirect: true } as any)
      user.value = res.data
    } catch {
      token.value = null
      localStorage.removeItem('token')
    }
  }

  async function logout() {
    // Explicit logout: a 401 from this POST is expected (the cookie may
    // already be cleared, or the request races the server's invalidate).
    // Opt out of the global 401 handler so logout doesn't recurse into
    // itself and surface a "session expired" toast for what the user
    // intentionally triggered.
    try {
      await api.post('/api/v2/auth/logout', undefined, { _skipAuthRedirect: true } as any)
    } catch {
      // best-effort — clear local state regardless
    }
    token.value = null
    user.value = null
    localStorage.removeItem('token')
  }

  return { token, user, isAuthenticated, isAdmin, login, register, fetchProfile, bootstrap, logout }
})
