import axios from 'axios'

export const api = axios.create({
  baseURL: '/',
  headers: {
    'Content-Type': 'application/json',
  },
  withCredentials: true,
})

// Attach auth token from localStorage as fallback (API tokens, legacy sessions)
api.interceptors.request.use((config) => {
  const token = localStorage.getItem('token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// Handle 401 responses globally. Callers that probe auth state on boot
// (the SSO/cookie session check) set `_skipAuthRedirect: true` to opt out:
// a 401 there just means "not logged in", not "session expired."
api.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401 && !error.config?._skipAuthRedirect) {
      localStorage.removeItem('token')
      // Clear server-side cookie too (best-effort)
      api.post('/api/v2/auth/logout').catch(() => {})
      window.dispatchEvent(new CustomEvent('session-expired'))
      setTimeout(() => { window.location.href = '/login' }, 1500)
    }
    return Promise.reject(error)
  }
)

// JWT expiry checking
function getTokenExpiry(): number | null {
  const token = localStorage.getItem('token')
  if (!token) return null
  try {
    const payload = JSON.parse(atob(token.split('.')[1]))
    return payload.exp ? payload.exp * 1000 : null
  } catch {
    return null
  }
}

let expiryWarned = false
setInterval(() => {
  const expiry = getTokenExpiry()
  if (!expiry) return
  const remaining = expiry - Date.now()
  if (remaining <= 0) {
    localStorage.removeItem('token')
    window.dispatchEvent(new CustomEvent('session-expired'))
    setTimeout(() => { window.location.href = '/login' }, 1500)
  } else if (remaining < 5 * 60 * 1000 && !expiryWarned) {
    expiryWarned = true
    window.dispatchEvent(new CustomEvent('session-expiring', {
      detail: { minutes: Math.ceil(remaining / 60000) },
    }))
  }
}, 30000)

export function resetExpiryWarning() {
  expiryWarned = false
}
