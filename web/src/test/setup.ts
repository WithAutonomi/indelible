import { config } from '@vue/test-utils'
import { vi } from 'vitest'

// Mock localStorage
const storage: Record<string, string> = {}
vi.stubGlobal('localStorage', {
  getItem: vi.fn((key: string) => storage[key] ?? null),
  setItem: vi.fn((key: string, val: string) => { storage[key] = val }),
  removeItem: vi.fn((key: string) => { delete storage[key] }),
  clear: vi.fn(() => { Object.keys(storage).forEach(k => delete storage[k]) }),
})

// Stub PrimeVue components globally to avoid importing the full library
config.global.stubs = {
  InputText: true,
  Password: true,
  Button: true,
  Message: true,
  DataTable: true,
  Column: true,
  Tag: true,
  Card: true,
  Dialog: true,
  Toast: true,
  ConfirmDialog: true,
  Select: true,
  Avatar: true,
  AutoComplete: true,
  Skeleton: true,
  Textarea: true,
  Checkbox: true,
  ProgressBar: true,
}

// Stub PrimeVue services
config.global.mocks = {
  $toast: { add: vi.fn() },
  $confirm: { require: vi.fn() },
}
