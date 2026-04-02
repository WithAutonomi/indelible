import { ref } from 'vue'
import { useToast } from 'primevue/usetoast'

interface ActionOptions {
  successSummary?: string
  successDetail?: string
  errorDetail?: string
}

/**
 * Wraps an async API call with loading state, success toast, and error toast.
 * Returns { loading, execute } where execute() runs the action.
 */
export function useApiAction(opts: ActionOptions = {}) {
  const toast = useToast()
  const loading = ref(false)

  async function execute<T>(fn: () => Promise<T>): Promise<T | undefined> {
    loading.value = true
    try {
      const result = await fn()
      if (opts.successSummary) {
        toast.add({
          severity: 'success',
          summary: opts.successSummary,
          detail: opts.successDetail || '',
          life: 3000,
        })
      }
      return result
    } catch (e: any) {
      toast.add({
        severity: 'error',
        summary: 'Error',
        detail: e.response?.data?.error || opts.errorDetail || 'Operation failed',
        life: 5000,
      })
      return undefined
    } finally {
      loading.value = false
    }
  }

  return { loading, execute }
}

/**
 * Shorthand: fire an API action and show toast, without tracking loading state.
 */
export function useToastAction() {
  const toast = useToast()

  async function run(
    fn: () => Promise<any>,
    success?: { summary: string; detail: string },
    errorDetail?: string,
  ): Promise<boolean> {
    try {
      await fn()
      if (success) {
        toast.add({ severity: 'success', summary: success.summary, detail: success.detail, life: 3000 })
      }
      return true
    } catch (e: any) {
      toast.add({
        severity: 'error',
        summary: 'Error',
        detail: e.response?.data?.error || errorDetail || 'Operation failed',
        life: 5000,
      })
      return false
    }
  }

  return { run }
}
