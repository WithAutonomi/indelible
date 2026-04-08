import { describe, it, expect, vi, beforeEach } from 'vitest'

vi.mock('../../api/client', () => ({
  api: {
    get: vi.fn(),
  },
}))

import { api } from '../../api/client'
import { usePagination } from '../usePagination'

const mockGet = api.get as ReturnType<typeof vi.fn>

describe('usePagination', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('fetches first page with correct params', async () => {
    mockGet.mockResolvedValueOnce({
      data: { uploads: [{ id: 1 }, { id: 2 }], total: 50 },
    })

    const { items, total, loading, fetch } = usePagination<any>({
      url: '/api/v2/uploads',
      key: 'uploads',
      limit: 20,
    })

    await fetch()

    expect(mockGet).toHaveBeenCalledWith('/api/v2/uploads', {
      params: { limit: 20, offset: 0 },
    })
    expect(items.value).toEqual([{ id: 1 }, { id: 2 }])
    expect(total.value).toBe(50)
    expect(loading.value).toBe(false)
  })

  it('handles page change', async () => {
    mockGet.mockResolvedValue({
      data: { items: [{ id: 3 }], total: 50 },
    })

    const { onPage, page } = usePagination<any>({
      url: '/api/v2/items',
      key: 'items',
      limit: 10,
    })

    onPage({ page: 2 }) // PrimeVue pages are 0-indexed

    expect(page.value).toBe(3)
    expect(mockGet).toHaveBeenCalledWith('/api/v2/items', {
      params: expect.objectContaining({ offset: 20 }),
    })
  })

  it('passes extra params', async () => {
    mockGet.mockResolvedValueOnce({
      data: { results: [], total: 0 },
    })

    const { fetch } = usePagination<any>({
      url: '/api/v2/search',
      key: 'results',
      params: () => ({ q: 'test', status: 'active' }),
    })

    await fetch()

    expect(mockGet).toHaveBeenCalledWith('/api/v2/search', {
      params: expect.objectContaining({ q: 'test', status: 'active' }),
    })
  })

  it('applies transform function', async () => {
    mockGet.mockResolvedValueOnce({
      data: { items: [{ name: 'a' }, { name: 'b' }], total: 2 },
    })

    const { items, fetch } = usePagination<any>({
      url: '/api/v2/items',
      key: 'items',
      transform: (list) => list.map(i => ({ ...i, upper: i.name.toUpperCase() })),
    })

    await fetch()

    expect(items.value[0].upper).toBe('A')
    expect(items.value[1].upper).toBe('B')
  })

  it('handles API error gracefully', async () => {
    mockGet.mockRejectedValueOnce(new Error('network'))

    const { items, total, fetch } = usePagination<any>({
      url: '/api/v2/items',
      key: 'items',
    })

    await fetch()

    expect(items.value).toEqual([])
    expect(total.value).toBe(0)
  })
})
