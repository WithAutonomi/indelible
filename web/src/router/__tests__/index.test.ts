import { describe, it, expect, vi, beforeEach } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

vi.mock('../../api/client', () => ({
  api: { post: vi.fn(), get: vi.fn() },
}))

describe('router guards', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    localStorage.clear()
  })

  it('redirects unauthenticated user to login for auth routes', async () => {
    const { default: router } = await import('../index')
    // Reset router to initial state
    await router.push('/login')
    await router.isReady()

    // Try to navigate to protected route without auth
    await router.push('/uploads')

    expect(router.currentRoute.value.name).toBe('login')
  })

  it('redirects authenticated user away from guest routes', async () => {
    // isAuthenticated derives from user, not token: SSO users have no token
    // but are fully authenticated via the session cookie.
    const { useAuthStore } = await import('../../stores/auth')
    const auth = useAuthStore()
    auth.user = { permissions: 'read' }

    const { default: router } = await import('../index')
    await router.push('/')
    await router.isReady()

    await router.push('/login')

    expect(router.currentRoute.value.name).toBe('dashboard')
  })

  it('redirects non-admin away from admin routes', async () => {
    const { useAuthStore } = await import('../../stores/auth')
    const auth = useAuthStore()
    auth.user = { permissions: 'read' }

    const { default: router } = await import('../index')
    await router.push('/')
    await router.isReady()

    await router.push('/admin/users')

    expect(router.currentRoute.value.name).toBe('dashboard')
  })

  it('allows admin to access admin routes', async () => {
    const { useAuthStore } = await import('../../stores/auth')
    const auth = useAuthStore()
    auth.user = { permissions: 'admin' }

    const { default: router } = await import('../index')
    await router.push('/')
    await router.isReady()

    await router.push('/admin/users')

    expect(router.currentRoute.value.name).toBe('admin-users')
  })
})
