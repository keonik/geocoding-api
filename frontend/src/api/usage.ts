import { fetchAPI } from '@/lib/api-client'
import type { APIResponse, UsageStats } from '@/types/api'

export const usageAPI = {
  getStats: async (): Promise<APIResponse<UsageStats>> => {
    return fetchAPI('/api/v1/user/usage')
  },
}
