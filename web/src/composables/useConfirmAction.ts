import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'

interface ConfirmActionOptions {
  message: string
  header?: string
  icon?: string
  successSummary?: string
  successDetail?: string
  errorDetail?: string
}

/**
 * Wraps a destructive action with PrimeVue ConfirmDialog + success/error toast.
 */
export function useConfirmAction() {
  const confirm = useConfirm()
  const toast = useToast()

  function confirmAndRun(opts: ConfirmActionOptions, fn: () => Promise<void>) {
    confirm.require({
      message: opts.message,
      header: opts.header || 'Confirm',
      icon: opts.icon || 'pi pi-exclamation-triangle',
      acceptClass: 'p-button-danger',
      accept: async () => {
        try {
          await fn()
          if (opts.successSummary) {
            toast.add({
              severity: 'success',
              summary: opts.successSummary,
              detail: opts.successDetail || '',
              life: 3000,
            })
          }
        } catch (e: any) {
          toast.add({
            severity: 'error',
            summary: 'Error',
            detail: e.response?.data?.error || opts.errorDetail || 'Operation failed',
            life: 5000,
          })
        }
      },
    })
  }

  return { confirmAndRun }
}
