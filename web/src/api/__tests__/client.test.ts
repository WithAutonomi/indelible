import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

describe('api client', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
    localStorage.clear()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('request interceptor attaches Bearer token', async () => {
    localStorage.setItem('token', 'test-jwt-token')

    // Import fresh to get interceptors
    const { api } = await import('../client')
    // @ts-ignore - access interceptor internals for testing
    const requestInterceptor = api.interceptors.request.handlers[0]
    const config = { headers: {} as Record<string, string> }
    const result = requestInterceptor.fulfilled(config)

    expect(result.headers.Authorization).toBe('Bearer test-jwt-token')
  })

  it('request interceptor skips auth when no token', async () => {
    const { api } = await import('../client')
    // @ts-ignore
    const requestInterceptor = api.interceptors.request.handlers[0]
    const config = { headers: {} as Record<string, string> }
    const result = requestInterceptor.fulfilled(config)

    expect(result.headers.Authorization).toBeUndefined()
  })

  it('response interceptor handles 401 by clearing token', async () => {
    localStorage.setItem('token', 'expired-token')
    const dispatchSpy = vi.spyOn(window, 'dispatchEvent')

    const { api } = await import('../client')
    // @ts-ignore
    const responseInterceptor = api.interceptors.response.handlers[0]

    const error = { response: { status: 401 } }
    await responseInterceptor.rejected(error).catch(() => {})

    expect(localStorage.removeItem).toHaveBeenCalledWith('token')
    expect(dispatchSpy).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'session-expired' })
    )
  })

  it('response interceptor ignores non-401 errors', async () => {
    const { api } = await import('../client')
    // @ts-ignore
    const responseInterceptor = api.interceptors.response.handlers[0]

    const error = { response: { status: 500 } }
    await expect(responseInterceptor.rejected(error)).rejects.toEqual(error)

    expect(localStorage.removeItem).not.toHaveBeenCalled()
  })
})
