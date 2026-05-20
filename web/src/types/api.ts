// Shared API response types for the Indelible frontend

export interface Upload {
  uuid: string
  filename: string
  original_filename: string
  file_size: number
  content_type: string
  visibility: 'public' | 'private'
  status: 'queued' | 'processing' | 'completed' | 'failed'
  status_detail?: string
  datamap_address?: string
  estimated_cost?: string
  actual_cost?: string
  error_message?: string
  backoff_until?: string
  backoff_attempt: number
  last_quoted_cost?: string
  queued_at: string
  processing_at?: string
  completed_at?: string
  failed_at?: string
  created_at: string
}

export interface Collection {
  id: number
  name: string
  description: string
  parent_id?: number
  file_count: number
  created_at: string
  updated_at: string
}

export interface CollectionFile {
  upload_uuid: string
  original_name: string
  added_at: string
}

export interface User {
  id: number
  email: string
  first_name: string
  last_name: string
  is_active: boolean
  is_service_account: boolean
  email_verified: boolean
  permissions: string
  last_login_at?: string
  created_at: string
}

export interface ApiToken {
  id: number
  uuid: string
  name: string
  token_prefix: string
  permissions: string
  request_count: number
  last_used_at?: string
  expires_at: string
  created_at: string
}

export interface Wallet {
  id: number
  name: string
  address: string
  is_default: boolean
  payment_balance: string
  gas_balance: string
  created_at: string
  updated_at: string
}

export interface Webhook {
  id: number
  url: string
  integration_type: 'generic' | 'slack'
  is_enabled: boolean
  events: string
  created_at: string
  updated_at: string
}

export interface WebhookDelivery {
  id: number
  webhook_id: number
  event_type: string
  status_code: number | null
  success: boolean
  attempts: number
  error_message?: string
  created_at: string
}

export interface Quota {
  id: number
  entity_type: 'system' | 'user' | 'group' | 'department'
  entity_id?: string
  max_bytes: number
  used_bytes: number
  is_enabled: boolean
  created_at: string
  updated_at: string
}

export interface ScimToken {
  id: number
  name: string
  is_active: boolean
  created_by: number
  last_used_at: string | null
  created_at: string
  revoked_at: string | null
}

export interface OidcProvider {
  id: number
  name: string
  display_name: string
  issuer_url: string
  client_id: string
  scopes: string
  is_enabled: boolean
  created_at: string
  updated_at: string
}

export interface AuditLogEntry {
  id: number
  event_type: string
  severity: 'info' | 'warn' | 'error'
  user_id?: number
  detail: string
  ip_address?: string
  user_agent?: string
  request_id: string
  created_at: string
}

export interface SystemLogEntry {
  id: number
  level: 'DEBUG' | 'INFO' | 'WARN' | 'ERROR'
  component: string
  message: string
  detail?: string
  request_id: string
  created_at: string
}

// Paginated list wrapper
export interface PaginatedList<T> {
  total: number
  limit: number
  offset: number
  items: T[]
}

// API error response
export interface ApiError {
  error: string
  code?: string
}
