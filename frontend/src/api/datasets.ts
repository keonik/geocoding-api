import { fetchAPI } from '@/lib/api-client'
import type { APIResponse } from '@/types/api'

export interface Dataset {
  id: number
  name: string
  state: string
  county: string
  file_type: string
  file_path: string
  file_size: number
  record_count: number
  status: 'pending' | 'processing' | 'completed' | 'failed'
  error_message?: string
  uploaded_by: number
  uploaded_at: string
  processed_at?: string
}

export interface DatasetStats {
  total_datasets: number
  total_records: number
  state_breakdown: Record<string, number>
  status_breakdown: Record<string, number>
  total_storage_size: number
}

export interface DatasetsListResponse {
  datasets: Dataset[]
  total: number
  limit: number
  offset: number
}

export const datasetAPI = {
  upload: async (formData: FormData): Promise<APIResponse<Dataset>> => {
    return fetchAPI('/api/v1/admin/datasets/upload', {
      method: 'POST',
      headers: {}, // Let browser set Content-Type for multipart/form-data
      body: formData,
    })
  },

  list: async (params?: {
    state?: string
    status?: string
    limit?: number
    offset?: number
  }): Promise<APIResponse<DatasetsListResponse>> => {
    const searchParams = new URLSearchParams()
    if (params?.state) searchParams.set('state', params.state)
    if (params?.status) searchParams.set('status', params.status)
    if (params?.limit) searchParams.set('limit', params.limit.toString())
    if (params?.offset) searchParams.set('offset', params.offset.toString())
    
    const query = searchParams.toString()
    return fetchAPI(`/api/v1/admin/datasets${query ? `?${query}` : ''}`)
  },

  get: async (id: number): Promise<APIResponse<Dataset>> => {
    return fetchAPI(`/api/v1/admin/datasets/${id}`)
  },

  delete: async (id: number): Promise<APIResponse<void>> => {
    return fetchAPI(`/api/v1/admin/datasets/${id}`, {
      method: 'DELETE',
    })
  },

  reprocess: async (id: number): Promise<APIResponse<void>> => {
    return fetchAPI(`/api/v1/admin/datasets/${id}/reprocess`, {
      method: 'POST',
    })
  },

  getStats: async (): Promise<APIResponse<DatasetStats>> => {
    return fetchAPI('/api/v1/admin/datasets/stats')
  },
}
