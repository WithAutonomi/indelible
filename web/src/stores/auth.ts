import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { api } from '../api/client'

export const useAuthStore = defineStore('auth', () => {
  const user = ref<any>(null)

  // Auth state is the loaded user profile: the JWT lives in an HttpOnly
  // session cookie the SPA can't read. Boot resolves the cookie by calling
  // /me; subsequent calls auto-attach the cookie via withCredentials.
  const isAuthenticated = computed(() => !!user.value)
  const isAdmin = computed(() => user.value?.permissions?.includes('admin') ?? false)

  async function login(email: string, password: string) {
    await api.post('/api/v2/auth/login', { email, password })
    // Server sets the session + csrf_token cookies on the response.
    // The JWT in the response body is legacy and intentionally ignored.
    await fetchProfile()
  }

  async function register(email: string, password: string, firstName: string, lastName: string) {
    // Registration is anti-enumeration (V2-430): the server returns a neutral
    // response and does NOT log the user in (no session cookie). The caller
    // routes to the login page; we deliberately don't fetchProfile here.
    await api.post('/api/v2/auth/register', {
      email,
      password,
      first_name: firstName,
      last_name: lastName,
    })
  }

  async function fetchProfile() {
    const res = await api.get('/api/v2/me')
    user.value = res.data
  }

  // Called from main.ts before mount. Tries to resolve the current session
  // from the HttpOnly cookie. Swallows 401: a missing/expired cookie just
  // means "not logged in" — opt out of the global 401 redirect-to-/login
  // behavior so the bootstrap probe doesn't loop on every page load.
  async function bootstrap() {
    if (user.value) return
    try {
      const res = await api.get('/api/v2/me', { _skipAuthRedirect: true } as any)
      user.value = res.data
    } catch {
      user.value = null
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
    user.value = null
  }

  return { user, isAuthenticated, isAdmin, login, register, fetchProfile, bootstrap, logout }
})
