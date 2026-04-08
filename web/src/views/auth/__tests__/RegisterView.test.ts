import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import RegisterView from '../RegisterView.vue'

vi.mock('../../../api/client', () => ({
  api: { post: vi.fn(), get: vi.fn() },
}))

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/register', name: 'register', component: RegisterView },
      { path: '/', name: 'dashboard', component: { template: '<div />' } },
      { path: '/login', name: 'login', component: { template: '<div />' } },
    ],
  })
}

describe('RegisterView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    localStorage.clear()
  })

  it('renders registration form', () => {
    const router = createTestRouter()
    const wrapper = mount(RegisterView, {
      global: { plugins: [router, createPinia()] },
    })

    expect(wrapper.find('form').exists()).toBe(true)
    expect(wrapper.text()).toContain('Create your account')
    expect(wrapper.text()).toContain('First name')
    expect(wrapper.text()).toContain('Last name')
    expect(wrapper.text()).toContain('Email')
    expect(wrapper.text()).toContain('Password')
  })

  it('calls auth.register on form submit', async () => {
    const { api } = await import('../../../api/client')
    const mockApi = api as any
    mockApi.post.mockResolvedValueOnce({ data: { token: 'jwt' } })
    mockApi.get.mockResolvedValueOnce({ data: { id: 1 } })

    const router = createTestRouter()
    await router.push('/register')
    await router.isReady()

    const wrapper = mount(RegisterView, {
      global: { plugins: [router, createPinia()] },
    })

    // Set reactive data directly since PrimeVue inputs are stubbed
    const vm = wrapper.vm as any
    vm.email = 'new@test.com'
    vm.password = 'password123'
    vm.firstName = 'Test'
    vm.lastName = 'User'

    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(mockApi.post).toHaveBeenCalledWith('/api/v2/auth/register', {
      email: 'new@test.com',
      password: 'password123',
      first_name: 'Test',
      last_name: 'User',
    })
  })

  it('shows error on registration failure', async () => {
    const { api } = await import('../../../api/client')
    const mockApi = api as any
    mockApi.post.mockRejectedValueOnce({
      response: { data: { error: 'Email already exists' } },
    })

    const router = createTestRouter()
    await router.push('/register')
    await router.isReady()

    const wrapper = mount(RegisterView, {
      global: { plugins: [router, createPinia()] },
    })

    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect((wrapper.vm as any).error).toBe('Email already exists')
  })
})
