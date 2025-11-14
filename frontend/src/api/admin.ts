import { fetchAPI } from '@/lib/api-client'
import type { APIResponse } from '@/types/api'

export interface AdminStats {
  total_users: number
  active_keys: number
  calls_today: number
  zip_codes: number
}

export interface AdminUser {
  id: number
  email: string
  name: string
  company?: string
  plan_type: string
  is_active: boolean
  is_admin: boolean
  created_at: string
}

export interface AdminAPIKey {
  id: number
  user_email: string
  name: string
  key_preview: string
  is_active: boolean
  last_used_at?: string
  created_at: string
}

export interface SystemStatus {
  database_connected: boolean
  migrations_current: boolean
}

export const adminAPI = {
  getStats: async (): Promise<APIResponse<AdminStats>> => {
    return fetchAPI('/api/v1/admin/stats')
  },

  getUsers: async (): Promise<APIResponse<AdminUser[]>> => {
    return fetchAPI('/api/v1/admin/users')
  },

  getAPIKeys: async (): Promise<APIResponse<AdminAPIKey[]>> => {
    return fetchAPI('/api/v1/admin/api-keys')
  },

  getSystemStatus: async (): Promise<APIResponse<SystemStatus>> => {
    return fetchAPI('/api/v1/admin/system-status')
  },

  updateUserStatus: async (
    userId: number,
    isActive: boolean
  ): Promise<APIResponse<void>> => {
    return fetchAPI(`/api/v1/admin/users/${userId}/status`, {
      method: 'PUT',
      body: JSON.stringify({ is_active: isActive }),
    })
  },

  updateUserAdmin: async (
    userId: number,
    isAdmin: boolean
  ): Promise<APIResponse<void>> => {
    return fetchAPI(`/api/v1/admin/users/${userId}/admin`, {
      method: 'PUT',
      body: JSON.stringify({ is_admin: isAdmin }),
    })
  },

  loadData: async (): Promise<APIResponse<void>> => {
    return fetchAPI('/api/v1/admin/load-data', {
      method: 'POST',
    })
  },
}
