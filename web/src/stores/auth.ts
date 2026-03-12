import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { api } from '../api/client'

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string | null>(localStorage.getItem('token'))
  const user = ref<any>(null)

  const isAuthenticated = computed(() => !!token.value)
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

  function logout() {
    token.value = null
    user.value = null
    localStorage.removeItem('token')
  }

  return { token, user, isAuthenticated, isAdmin, login, register, fetchProfile, logout }
})
