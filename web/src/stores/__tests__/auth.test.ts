import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useAuthStore } from '../auth'

vi.mock('../../api/client', () => ({
  api: {
    post: vi.fn(),
    get: vi.fn(),
  },
}))

import { api } from '../../api/client'

const mockApi = api as unknown as {
  post: ReturnType<typeof vi.fn>
  get: ReturnType<typeof vi.fn>
}

describe('auth store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    localStorage.clear()
  })

  it('login sets token and fetches profile', async () => {
    mockApi.post.mockResolvedValueOnce({ data: { token: 'jwt-test-token' } })
    mockApi.get.mockResolvedValueOnce({
      data: { id: 1, email: 'user@test.com', permissions: 'read' },
    })

    const auth = useAuthStore()
    await auth.login('user@test.com', 'password123')

    expect(mockApi.post).toHaveBeenCalledWith('/api/v2/auth/login', {
      email: 'user@test.com',
      password: 'password123',
    })
    expect(auth.token).toBe('jwt-test-token')
    expect(localStorage.setItem).toHaveBeenCalledWith('token', 'jwt-test-token')
    expect(auth.user).toEqual({ id: 1, email: 'user@test.com', permissions: 'read' })
  })

  it('register sets token and fetches profile', async () => {
    mockApi.post.mockResolvedValueOnce({ data: { token: 'jwt-register-token' } })
    mockApi.get.mockResolvedValueOnce({
      data: { id: 2, email: 'new@test.com', permissions: 'read' },
    })

    const auth = useAuthStore()
    await auth.register('new@test.com', 'password123', 'Test', 'User')

    expect(mockApi.post).toHaveBeenCalledWith('/api/v2/auth/register', {
      email: 'new@test.com',
      password: 'password123',
      first_name: 'Test',
      last_name: 'User',
    })
    expect(auth.token).toBe('jwt-register-token')
    expect(auth.user).toEqual({ id: 2, email: 'new@test.com', permissions: 'read' })
  })

  it('logout clears token and user', async () => {
    mockApi.post.mockResolvedValueOnce({ data: { token: 'jwt-token' } })
    mockApi.get.mockResolvedValueOnce({
      data: { id: 1, email: 'user@test.com', permissions: 'read' },
    })

    const auth = useAuthStore()
    await auth.login('user@test.com', 'password123')

    mockApi.post.mockResolvedValueOnce({})
    await auth.logout()

    expect(auth.token).toBeNull()
    expect(auth.user).toBeNull()
    expect(localStorage.removeItem).toHaveBeenCalledWith('token')
  })

  it('logout clears state even if API call fails', async () => {
    const auth = useAuthStore()
    auth.token = 'some-token'
    auth.user = { id: 1 }

    mockApi.post.mockRejectedValueOnce(new Error('network error'))
    await auth.logout()

    expect(auth.token).toBeNull()
    expect(auth.user).toBeNull()
  })

  it('isAuthenticated is true when token exists', () => {
    const auth = useAuthStore()
    expect(auth.isAuthenticated).toBe(false)

    auth.token = 'some-token'
    expect(auth.isAuthenticated).toBe(true)
  })

  it('isAdmin checks permissions array', () => {
    const auth = useAuthStore()
    expect(auth.isAdmin).toBe(false)

    auth.user = { permissions: 'read' }
    expect(auth.isAdmin).toBe(false)

    auth.user = { permissions: 'admin' }
    expect(auth.isAdmin).toBe(true)
  })

  it('fetchProfile sets user from API', async () => {
    mockApi.get.mockResolvedValueOnce({
      data: { id: 1, email: 'user@test.com', first_name: 'Test' },
    })

    const auth = useAuthStore()
    await auth.fetchProfile()

    expect(mockApi.get).toHaveBeenCalledWith('/api/v2/me')
    expect(auth.user).toEqual({ id: 1, email: 'user@test.com', first_name: 'Test' })
  })
})
