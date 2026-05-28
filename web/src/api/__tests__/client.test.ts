import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

describe('api client', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
    // Clear document.cookie. jsdom keeps cookies between tests otherwise.
    document.cookie.split(';').forEach((c) => {
      const eq = c.indexOf('=')
      const name = (eq > -1 ? c.slice(0, eq) : c).trim()
      if (name) {
        document.cookie = `${name}=; expires=Thu, 01 Jan 1970 00:00:00 GMT; path=/`
      }
    })
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('attaches X-CSRF-Token on POST when csrf_token cookie is present', async () => {
    document.cookie = 'csrf_token=csrf-test-value; path=/'

    const { api } = await import('../client')
    // @ts-ignore - axios internal interceptor handle for white-box testing
    const requestInterceptor = api.interceptors.request.handlers[0]
    const config = { method: 'post', headers: {} as Record<string, string> }
    const result = requestInterceptor.fulfilled(config)

    expect(result.headers['X-CSRF-Token']).toBe('csrf-test-value')
  })

  it('does not attach X-CSRF-Token on GET', async () => {
    document.cookie = 'csrf_token=csrf-test-value; path=/'

    const { api } = await import('../client')
    // @ts-ignore
    const requestInterceptor = api.interceptors.request.handlers[0]
    const config = { method: 'get', headers: {} as Record<string, string> }
    const result = requestInterceptor.fulfilled(config)

    expect(result.headers['X-CSRF-Token']).toBeUndefined()
  })

  it('skips X-CSRF-Token when no csrf_token cookie is set', async () => {
    const { api } = await import('../client')
    // @ts-ignore
    const requestInterceptor = api.interceptors.request.handlers[0]
    const config = { method: 'post', headers: {} as Record<string, string> }
    const result = requestInterceptor.fulfilled(config)

    expect(result.headers['X-CSRF-Token']).toBeUndefined()
  })

  it('response interceptor handles 401 by dispatching session-expired', async () => {
    const dispatchSpy = vi.spyOn(window, 'dispatchEvent')

    const { api } = await import('../client')
    // @ts-ignore
    const responseInterceptor = api.interceptors.response.handlers[0]

    const error = { response: { status: 401 }, config: {} }
    await responseInterceptor.rejected(error).catch(() => {})

    expect(dispatchSpy).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'session-expired' })
    )
  })

  it('response interceptor skips redirect when _skipAuthRedirect is set', async () => {
    const dispatchSpy = vi.spyOn(window, 'dispatchEvent')

    const { api } = await import('../client')
    // @ts-ignore
    const responseInterceptor = api.interceptors.response.handlers[0]

    const error = { response: { status: 401 }, config: { _skipAuthRedirect: true } }
    await responseInterceptor.rejected(error).catch(() => {})

    expect(dispatchSpy).not.toHaveBeenCalled()
  })

  it('response interceptor ignores non-401 errors', async () => {
    const { api } = await import('../client')
    // @ts-ignore
    const responseInterceptor = api.interceptors.response.handlers[0]

    const error = { response: { status: 500 }, config: {} }
    await expect(responseInterceptor.rejected(error)).rejects.toEqual(error)
  })
})
