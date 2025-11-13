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
export interface UsageSummary {
  user_id: number
  month: string
  total_calls: number
  billable_calls: number
  total_cost: number
  endpoint_breakdown: Record<string, number>
}

export interface RateLimit {
  within_limit: boolean
  current_usage: number
  monthly_limit: number
  remaining: number
}

export interface UsageStats {
  usage_summary: UsageSummary
  rate_limit: RateLimit
}
