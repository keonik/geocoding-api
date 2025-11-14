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
}
