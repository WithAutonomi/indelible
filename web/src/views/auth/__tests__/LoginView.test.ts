import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import LoginView from '../LoginView.vue'

vi.mock('../../../api/client', () => ({
  api: { post: vi.fn(), get: vi.fn() },
}))

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/login', name: 'login', component: LoginView },
      { path: '/', name: 'dashboard', component: { template: '<div />' } },
      { path: '/register', name: 'register', component: { template: '<div />' } },
      { path: '/forgot-password', name: 'forgot-password', component: { template: '<div />' } },
    ],
  })
}

describe('LoginView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    localStorage.clear()
  })

  it('renders login form', () => {
    const router = createTestRouter()
    const wrapper = mount(LoginView, {
      global: { plugins: [router, createPinia()] },
    })

    expect(wrapper.find('form').exists()).toBe(true)
    expect(wrapper.text()).toContain('Sign in')
    expect(wrapper.text()).toContain('Email')
    expect(wrapper.text()).toContain('Password')
  })

  it('calls auth.login on form submit', async () => {
    const { api } = await import('../../../api/client')
    const mockApi = api as any
    mockApi.post.mockResolvedValueOnce({ data: { token: 'jwt' } })
    mockApi.get.mockResolvedValueOnce({ data: { id: 1 } })

    const router = createTestRouter()
    await router.push('/login')
    await router.isReady()

    const wrapper = mount(LoginView, {
      global: { plugins: [router, createPinia()] },
    })

    // Set reactive data directly since PrimeVue inputs are stubbed
    const vm = wrapper.vm as any
    vm.email = 'user@test.com'
    vm.password = 'password123'

    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(mockApi.post).toHaveBeenCalledWith('/api/v2/auth/login', {
      email: 'user@test.com',
      password: 'password123',
    })
  })

  it('shows error message on login failure', async () => {
    const { api } = await import('../../../api/client')
    const mockApi = api as any
    mockApi.post.mockRejectedValueOnce({
      response: { data: { error: 'Invalid credentials' } },
    })

    const router = createTestRouter()
    await router.push('/login')
    await router.isReady()

    const wrapper = mount(LoginView, {
      global: { plugins: [router, createPinia()] },
    })

    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect((wrapper.vm as any).error).toBe('Invalid credentials')
  })
})
