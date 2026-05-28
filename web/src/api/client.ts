import axios from 'axios'

export const api = axios.create({
  baseURL: '/',
  headers: {
    'Content-Type': 'application/json',
  },
  withCredentials: true,
})

// Read a cookie by name. The csrf_token cookie is the only non-HttpOnly
// cookie the server sets; the session JWT is HttpOnly and invisible here.
function readCookie(name: string): string | null {
  const prefix = `${name}=`
  for (const part of document.cookie.split('; ')) {
    if (part.startsWith(prefix)) {
      return decodeURIComponent(part.slice(prefix.length))
    }
  }
  return null
}

// Attach the CSRF double-submit token on every mutating request. Reads
// from the csrf_token cookie the server set at login/register/SSO time.
// The server's CSRF middleware compares this header to the cookie in
// constant time. GET / HEAD / OPTIONS skip the header (server is
// exempt for read methods).
api.interceptors.request.use((config) => {
  const method = (config.method || 'get').toLowerCase()
  if (method === 'get' || method === 'head' || method === 'options') {
    return config
  }
  const csrf = readCookie('csrf_token')
  if (csrf) {
    config.headers['X-CSRF-Token'] = csrf
  }
  return config
})

// Handle 401 responses globally. Callers that probe auth state on boot
// (the SSO/cookie session check) or that intentionally tear down the
// session (logout) set `_skipAuthRedirect: true` to opt out: a 401 there
// just means "not logged in", not "session expired."
api.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401 && !error.config?._skipAuthRedirect) {
      // Best-effort cookie clear; server cookies remain authoritative.
      api.post('/api/v2/auth/logout', undefined, { _skipAuthRedirect: true } as any).catch(() => {})
      window.dispatchEvent(new CustomEvent('session-expired'))
      setTimeout(() => { window.location.href = '/login' }, 1500)
    }
    return Promise.reject(error)
  }
)
