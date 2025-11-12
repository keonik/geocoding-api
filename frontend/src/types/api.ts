// API Response types
export interface APIResponse<T> {
  success: boolean
  data?: T
  error?: string
  message?: string
}

// User types
export interface User {
  id: number
  email: string
  name: string
  company?: string
  plan_type: string
  is_admin: boolean
  created_at: string
}

export interface LoginRequest {
  email: string
  password: string
}

export interface RegisterRequest {
  email: string
  password: string
  name: string
  company?: string
}

export interface AuthResponse {
  token: string
  user: User
}

// API Key types
export interface APIKey {
  id: string
  user_id: number
  name: string
  key_preview: string
  permissions: string[]
  created_at: string
  last_used_at?: string
}

export interface CreateAPIKeyRequest {
  name: string
  permissions: string[]
}

export interface CreateAPIKeyResponse {
  key_string: string
  api_key: APIKey
}

export interface APIKeysResponse {
  api_keys: APIKey[]
  count: number
}

// Usage types
export interface UsageStats {
  current_usage: number
  monthly_limit: number
  period_start: string
  period_end: string
}
