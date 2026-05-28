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
  })

  it('login posts credentials and fetches profile via cookie', async () => {
    mockApi.post.mockResolvedValueOnce({})
    mockApi.get.mockResolvedValueOnce({
      data: { id: 1, email: 'user@test.com', permissions: 'read' },
    })

    const auth = useAuthStore()
    await auth.login('user@test.com', 'password123')

    expect(mockApi.post).toHaveBeenCalledWith('/api/v2/auth/login', {
      email: 'user@test.com',
      password: 'password123',
    })
    // Profile is fetched via /me — the session cookie set by the login
    // response is automatically attached by the browser. No JWT is read
    // from the response body.
    expect(mockApi.get).toHaveBeenCalledWith('/api/v2/me')
    expect(auth.user).toEqual({ id: 1, email: 'user@test.com', permissions: 'read' })
  })

  it('register posts credentials and fetches profile via cookie', async () => {
    mockApi.post.mockResolvedValueOnce({})
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
    expect(auth.user).toEqual({ id: 2, email: 'new@test.com', permissions: 'read' })
  })

  it('logout clears user state', async () => {
    mockApi.post.mockResolvedValueOnce({})
    mockApi.get.mockResolvedValueOnce({
      data: { id: 1, email: 'user@test.com', permissions: 'read' },
    })

    const auth = useAuthStore()
    await auth.login('user@test.com', 'password123')

    mockApi.post.mockResolvedValueOnce({})
    await auth.logout()

    expect(auth.user).toBeNull()
    expect(auth.isAuthenticated).toBe(false)
  })

  it('logout clears state even if API call fails', async () => {
    const auth = useAuthStore()
    auth.user = { id: 1 }

    mockApi.post.mockRejectedValueOnce(new Error('network error'))
    await auth.logout()

    expect(auth.user).toBeNull()
  })

  it('logout opts out of the global 401 handler', async () => {
    // Logout often races the cookie clear and the POST itself can return
    // 401. Without _skipAuthRedirect, the interceptor recurses into another
    // logout + session-expired toast — the bug this flag prevents.
    mockApi.post.mockResolvedValueOnce({})
    const auth = useAuthStore()
    await auth.logout()

    expect(mockApi.post).toHaveBeenCalledWith(
      '/api/v2/auth/logout',
      undefined,
      expect.objectContaining({ _skipAuthRedirect: true })
    )
  })

  it('isAuthenticated reflects user presence', () => {
    const auth = useAuthStore()
    expect(auth.isAuthenticated).toBe(false)

    auth.user = { id: 1, email: 'user@test.com' }
    expect(auth.isAuthenticated).toBe(true)
  })

  it('bootstrap resolves session from /me via the session cookie', async () => {
    mockApi.get.mockResolvedValueOnce({
      data: { id: 7, email: 'cookie@test.com', permissions: 'read' },
    })

    const auth = useAuthStore()
    await auth.bootstrap()

    // Bootstrap passes a config that opts out of the global 401 redirect
    // interceptor — a 401 on boot means "not logged in yet", not "session
    // expired." See web/src/api/client.ts.
    expect(mockApi.get).toHaveBeenCalledWith('/api/v2/me', { _skipAuthRedirect: true })
    expect(auth.user).toEqual({ id: 7, email: 'cookie@test.com', permissions: 'read' })
    expect(auth.isAuthenticated).toBe(true)
  })

  it('bootstrap swallows 401 and leaves user null', async () => {
    mockApi.get.mockRejectedValueOnce(new Error('401'))

    const auth = useAuthStore()
    await auth.bootstrap()

    expect(auth.user).toBeNull()
    expect(auth.isAuthenticated).toBe(false)
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
