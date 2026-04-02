import { ref } from 'vue'
import { api } from '../api/client'

interface PaginationOptions<T> {
  url: string
  key: string
  limit?: number
  params?: () => Record<string, any>
  transform?: (items: T[]) => T[]
}

/**
 * Manages server-side pagination with loading state.
 */
export function usePagination<T>(opts: PaginationOptions<T>) {
  const items = ref<T[]>([]) as { value: T[] }
  const loading = ref(true)
  const page = ref(1)
  const total = ref(0)
  const limit = opts.limit || 20

  async function fetch() {
    loading.value = true
    try {
      const params: Record<string, any> = {
        limit,
        offset: (page.value - 1) * limit,
        ...opts.params?.(),
      }
      const res = await api.get(opts.url, { params })
      const raw = res.data[opts.key] || []
      items.value = opts.transform ? opts.transform(raw) : raw
      total.value = res.data.total || raw.length
    } catch {
      items.value = []
      total.value = 0
    } finally {
      loading.value = false
    }
  }

  function onPage(event: { page: number }) {
    page.value = event.page + 1
    fetch()
  }

  return { items, loading, page, total, limit, fetch, onPage }
}
