import { fetchAPI } from '@/lib/api-client'
import type { APIResponse, UsageStats } from '@/types/api'

export interface DailyUsage {
  date: string
  total_calls: number
  billable_calls: number
  unique_endpoints: number
}

export interface EndpointUsage {
  endpoint: string
  total_calls: number
  billable_calls: number
  avg_response_time: number
  success_count: number
  error_count: number
}

export interface KeyUsage {
  key_id: number
  name: string
  key_preview: string
  is_active: boolean
  total_calls: number
  billable_calls: number
  avg_response_time: number
  error_count: number
  /** null when the key made no calls inside the window */
  last_call: string | null
}

export const usageAPI = {
  getStats: async (): Promise<APIResponse<UsageStats>> => {
    return fetchAPI('/api/v1/user/usage')
  },

  getDailyUsage: async (days: number = 30): Promise<APIResponse<DailyUsage[]>> => {
    return fetchAPI(`/api/v1/user/usage/daily?days=${days}`)
  },

  getEndpointUsage: async (days: number = 30): Promise<APIResponse<EndpointUsage[]>> => {
    return fetchAPI(`/api/v1/user/usage/endpoints?days=${days}`)
  },

  getKeyUsage: async (days: number = 30): Promise<APIResponse<KeyUsage[]>> => {
    return fetchAPI(`/api/v1/user/usage/keys?days=${days}`)
  },
}
