import { describe, it, expect, vi, beforeEach } from 'vitest'

const mockToastAdd = vi.fn()
vi.mock('primevue/usetoast', () => ({
  useToast: () => ({ add: mockToastAdd }),
}))

import { useApiAction, useToastAction } from '../useApiAction'

describe('useApiAction', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('sets loading during execution', async () => {
    const { loading, execute } = useApiAction()

    let loadingDuringExec = false
    const result = execute(async () => {
      loadingDuringExec = loading.value
      return 'result'
    })

    // loading should be true during execution
    await result
    expect(loadingDuringExec).toBe(true)
    expect(loading.value).toBe(false)
  })

  it('returns result on success', async () => {
    const { execute } = useApiAction()
    const result = await execute(async () => 42)
    expect(result).toBe(42)
  })

  it('shows success toast when successSummary provided', async () => {
    const { execute } = useApiAction({ successSummary: 'Done!' })
    await execute(async () => 'ok')

    expect(mockToastAdd).toHaveBeenCalledWith(
      expect.objectContaining({ severity: 'success', summary: 'Done!' })
    )
  })

  it('does not show toast when no successSummary', async () => {
    const { execute } = useApiAction()
    await execute(async () => 'ok')

    expect(mockToastAdd).not.toHaveBeenCalled()
  })

  it('shows error toast on failure', async () => {
    const { execute } = useApiAction()
    const result = await execute(async () => {
      throw { response: { data: { error: 'Not found' } } }
    })

    expect(result).toBeUndefined()
    expect(mockToastAdd).toHaveBeenCalledWith(
      expect.objectContaining({ severity: 'error', detail: 'Not found' })
    )
  })

  it('uses default error message when no response error', async () => {
    const { execute } = useApiAction({ errorDetail: 'Custom error' })
    await execute(async () => { throw new Error('network') })

    expect(mockToastAdd).toHaveBeenCalledWith(
      expect.objectContaining({ detail: 'Custom error' })
    )
  })
})

describe('useToastAction', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('returns true on success', async () => {
    const { run } = useToastAction()
    const result = await run(async () => 'ok')
    expect(result).toBe(true)
  })

  it('shows success toast when provided', async () => {
    const { run } = useToastAction()
    await run(async () => 'ok', { summary: 'Saved', detail: 'Item saved' })

    expect(mockToastAdd).toHaveBeenCalledWith(
      expect.objectContaining({ severity: 'success', summary: 'Saved' })
    )
  })

  it('returns false on error', async () => {
    const { run } = useToastAction()
    const result = await run(async () => { throw new Error('fail') })
    expect(result).toBe(false)
  })
})
