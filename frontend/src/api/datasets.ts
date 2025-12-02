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

export interface BatchUploadResult {
  filename: string
  success: boolean
  error?: string
  dataset?: Dataset
}

export interface BulkUploadResponse {
  total_files: number
  success_count: number
  fail_count: number
  results: BatchUploadResult[]
  message: string
}

// SSE Event types for streaming upload (matches backend UploadProgressEvent)
export type StreamEventType = 'start' | 'processing' | 'file_saved' | 'file_error' | 'processing_started' | 'complete'

export interface StreamEvent {
  type: StreamEventType
  filename?: string
  message?: string
  file_index?: number
  total_files?: number
  success?: boolean
  error?: string
  dataset?: Dataset
  success_count?: number
  fail_count?: number
}

export interface StreamUploadCallbacks {
  onStart?: (total: number, message: string) => void
  onProcessing?: (filename: string, index: number, total: number, message: string) => void
  onFileSaved?: (filename: string, index: number, total: number, dataset?: Dataset) => void
  onFileError?: (filename: string, index: number, total: number, error: string) => void
  onProcessingStarted?: (message: string) => void
  onComplete?: (successCount: number, failCount: number, total: number, message: string) => void
  onError?: (error: Error) => void
}

export const datasetAPI = {
  upload: async (formData: FormData): Promise<APIResponse<Dataset>> => {
    return fetchAPI('/api/v1/admin/datasets/upload', {
      method: 'POST',
      headers: {}, // Let browser set Content-Type for multipart/form-data
      body: formData,
    })
  },

  uploadBulk: async (formData: FormData): Promise<APIResponse<BulkUploadResponse>> => {
    return fetchAPI('/api/v1/admin/datasets/upload-bulk', {
      method: 'POST',
      headers: {}, // Let browser set Content-Type for multipart/form-data
      body: formData,
    })
  },

  /**
   * Upload a single file to the single-file upload endpoint
   */
  uploadSingle: async (formData: FormData): Promise<APIResponse<Dataset>> => {
    return fetchAPI('/api/v1/admin/datasets/upload', {
      method: 'POST',
      headers: {},
      body: formData,
    })
  },

  /**
   * Upload files in batches to prevent memory exhaustion and timeouts
   * Uploads files one at a time with progress callbacks
   * Includes retry logic and timeout handling
   */
  uploadBatched: async (
    files: File[],
    state: string,
    callbacks: {
      onFileStart: (filename: string, index: number, total: number) => void
      onFileProgress: (filename: string, loaded: number, total: number, percent: number) => void
      onFileComplete: (filename: string, index: number, success: boolean, result?: Dataset, error?: string) => void
      onAllComplete: (successCount: number, failCount: number, total: number) => void
      onError: (error: Error) => void
    },
    abortSignal?: { aborted: boolean }
  ): Promise<void> => {
    const token = localStorage.getItem('authToken')
    let successCount = 0
    let failCount = 0
    
    // Helper function to upload a single file with retry logic
    const uploadSingleFile = async (
      file: File, 
      retryCount = 0
    ): Promise<APIResponse<Dataset>> => {
      const maxRetries = 2
      const timeout = 10 * 60 * 1000 // 10 minutes per file (for very large files)
      
      // Extract county name from filename
      const countyMatch = file.name.match(/^([a-zA-Z]+)/i)
      const county = countyMatch 
        ? countyMatch[1].charAt(0).toUpperCase() + countyMatch[1].slice(1).toLowerCase()
        : 'Unknown'
      
      const formData = new FormData()
      formData.append('name', `${county} County Addresses`)
      formData.append('state', state)
      formData.append('county', county)
      formData.append('file', file)
      
      return new Promise<APIResponse<Dataset>>((resolve, reject) => {
        const xhr = new XMLHttpRequest()
        let timeoutId: ReturnType<typeof setTimeout> | null = null
        let lastProgressTime = Date.now()
        let progressCheckInterval: ReturnType<typeof setInterval> | null = null
        
        // Set up stall detection - if no progress for 2 minutes, retry
        const stallTimeout = 2 * 60 * 1000 // 2 minutes
        progressCheckInterval = setInterval(() => {
          const now = Date.now()
          if (now - lastProgressTime > stallTimeout) {
            console.warn(`[Upload] Stall detected for ${file.name}, no progress for 2 minutes`)
            if (progressCheckInterval) clearInterval(progressCheckInterval)
            if (timeoutId) clearTimeout(timeoutId)
            xhr.abort()
            
            if (retryCount < maxRetries) {
              console.log(`[Upload] Retrying ${file.name} (attempt ${retryCount + 2}/${maxRetries + 1})`)
              // Wait a bit before retrying
              setTimeout(() => {
                uploadSingleFile(file, retryCount + 1)
                  .then(resolve)
                  .catch(reject)
              }, 1000)
            } else {
              resolve({ success: false, error: 'Upload stalled - connection timeout' })
            }
          }
        }, 10000) // Check every 10 seconds
        
        xhr.upload.onprogress = (event) => {
          lastProgressTime = Date.now()
          if (event.lengthComputable) {
            const percent = Math.round((event.loaded / event.total) * 100)
            callbacks.onFileProgress(file.name, event.loaded, event.total, percent)
            
            // When upload reaches 100%, show that we're waiting for server
            if (percent === 100) {
              // Signal that we're now in "saving" phase
              callbacks.onFileProgress(file.name, event.loaded, event.total, 100)
            }
          }
        }
        
        // When all bytes are sent, we enter the "saving" phase
        xhr.upload.onload = () => {
          lastProgressTime = Date.now() // Reset timer since we're now in server processing phase
          // Signal saving phase - use -1 as a special marker
          callbacks.onFileProgress(file.name, -1, -1, 100)
        }
        
        xhr.onload = () => {
          if (progressCheckInterval) clearInterval(progressCheckInterval)
          if (timeoutId) clearTimeout(timeoutId)
          
          try {
            const response = JSON.parse(xhr.responseText)
            if (xhr.status >= 200 && xhr.status < 300) {
              resolve({ success: true, data: response.data || response })
            } else {
              resolve({ success: false, error: response.error || response.message || 'Upload failed' })
            }
          } catch (e) {
            resolve({ success: false, error: 'Failed to parse server response' })
          }
        }
        
        xhr.onerror = () => {
          if (progressCheckInterval) clearInterval(progressCheckInterval)
          if (timeoutId) clearTimeout(timeoutId)
          
          if (retryCount < maxRetries) {
            console.log(`[Upload] Network error for ${file.name}, retrying (attempt ${retryCount + 2}/${maxRetries + 1})`)
            setTimeout(() => {
              uploadSingleFile(file, retryCount + 1)
                .then(resolve)
                .catch(reject)
            }, 1000)
          } else {
            resolve({ success: false, error: 'Network error after retries' })
          }
        }
        
        xhr.onabort = () => {
          if (progressCheckInterval) clearInterval(progressCheckInterval)
          if (timeoutId) clearTimeout(timeoutId)
          reject(new Error('Upload cancelled'))
        }
        
        xhr.ontimeout = () => {
          if (progressCheckInterval) clearInterval(progressCheckInterval)
          if (timeoutId) clearTimeout(timeoutId)
          
          if (retryCount < maxRetries) {
            console.log(`[Upload] Timeout for ${file.name}, retrying (attempt ${retryCount + 2}/${maxRetries + 1})`)
            setTimeout(() => {
              uploadSingleFile(file, retryCount + 1)
                .then(resolve)
                .catch(reject)
            }, 1000)
          } else {
            resolve({ success: false, error: 'Upload timeout after retries' })
          }
        }
        
        xhr.open('POST', '/api/v1/admin/datasets/upload')
        xhr.timeout = timeout
        if (token) {
          xhr.setRequestHeader('Authorization', `Bearer ${token}`)
        }
        xhr.send(formData)
        
        // Also set overall timeout as backup
        timeoutId = setTimeout(() => {
          if (progressCheckInterval) clearInterval(progressCheckInterval)
          console.warn(`[Upload] Overall timeout for ${file.name}`)
          xhr.abort()
        }, timeout)
        
        // Store abort function for external cancellation
        if (abortSignal) {
          const checkAbort = setInterval(() => {
            if (abortSignal.aborted) {
              if (progressCheckInterval) clearInterval(progressCheckInterval)
              if (timeoutId) clearTimeout(timeoutId)
              xhr.abort()
              clearInterval(checkAbort)
            }
          }, 100)
          xhr.onloadend = () => clearInterval(checkAbort)
        }
      })
    }
    
    for (let i = 0; i < files.length; i++) {
      // Check if aborted
      if (abortSignal?.aborted) {
        callbacks.onError(new Error('Upload cancelled'))
        return
      }
      
      const file = files[i]
      callbacks.onFileStart(file.name, i, files.length)
      
      try {
        const result = await uploadSingleFile(file)
        
        if (result.success && result.data) {
          successCount++
          callbacks.onFileComplete(file.name, i, true, result.data)
        } else {
          failCount++
          callbacks.onFileComplete(file.name, i, false, undefined, result.error)
        }
      } catch (error) {
        failCount++
        const errorMessage = error instanceof Error ? error.message : 'Unknown error'
        callbacks.onFileComplete(file.name, i, false, undefined, errorMessage)
        
        if (errorMessage === 'Upload cancelled') {
          return
        }
      }
      
      // Small delay between files to let server breathe
      if (i < files.length - 1) {
        await new Promise(resolve => setTimeout(resolve, 200))
      }
    }
    
    callbacks.onAllComplete(successCount, failCount, files.length)
  },

  /**
   * Upload multiple files with real upload progress tracking
   * Uses XMLHttpRequest to get upload progress events
   */
  uploadBulkWithProgress: (
    formData: FormData,
    onProgress: (loaded: number, total: number, percent: number) => void,
    onComplete: (response: APIResponse<BulkUploadResponse>) => void,
    onError: (error: Error) => void
  ): { abort: () => void } => {
    const xhr = new XMLHttpRequest()
    
    // Get auth token
    const token = localStorage.getItem('authToken')
    
    // Track upload progress
    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable) {
        const percent = Math.round((event.loaded / event.total) * 100)
        onProgress(event.loaded, event.total, percent)
      }
    }
    
    // Handle completion
    xhr.onload = () => {
      try {
        const response = JSON.parse(xhr.responseText)
        if (xhr.status >= 200 && xhr.status < 300) {
          onComplete({ success: true, data: response.data || response })
        } else {
          onComplete({ success: false, error: response.error || response.message || 'Upload failed' })
        }
      } catch (e) {
        onError(new Error('Failed to parse server response'))
      }
    }
    
    // Handle errors
    xhr.onerror = () => {
      onError(new Error('Network error during upload'))
    }
    
    xhr.onabort = () => {
      onError(new Error('Upload cancelled'))
    }
    
    // Open and send
    xhr.open('POST', '/api/v1/admin/datasets/upload-bulk')
    if (token) {
      xhr.setRequestHeader('Authorization', `Bearer ${token}`)
    }
    xhr.send(formData)
    
    return {
      abort: () => xhr.abort()
    }
  },

  /**
   * Upload multiple files with SSE streaming progress updates
   * Returns an AbortController so the upload can be cancelled
   */
  uploadBulkStream: (formData: FormData, callbacks: StreamUploadCallbacks): AbortController => {
    const controller = new AbortController()
    
    // Get auth token from localStorage
    const token = localStorage.getItem('authToken')
    
    fetch('/api/v1/admin/datasets/upload-bulk-stream', {
      method: 'POST',
      headers: token ? { 'Authorization': `Bearer ${token}` } : {},
      body: formData,
      signal: controller.signal,
    })
      .then(async (response) => {
        if (!response.ok) {
          const errorText = await response.text()
          throw new Error(`Upload failed: ${response.status} ${errorText}`)
        }
        
        if (!response.body) {
          throw new Error('No response body for streaming')
        }
        
        const reader = response.body.getReader()
        const decoder = new TextDecoder()
        let buffer = ''
        
        while (true) {
          const { done, value } = await reader.read()
          
          if (done) break
          
          buffer += decoder.decode(value, { stream: true })
          
          // Process complete SSE messages
          const lines = buffer.split('\n')
          buffer = lines.pop() || '' // Keep incomplete line in buffer
          
          for (const line of lines) {
            if (line.startsWith('data: ')) {
              try {
                const event: StreamEvent = JSON.parse(line.slice(6))
                
                switch (event.type) {
                  case 'start':
                    callbacks.onStart?.(event.total_files || 0, event.message || '')
                    break
                  case 'processing':
                    callbacks.onProcessing?.(event.filename || '', event.file_index || 0, event.total_files || 0, event.message || '')
                    break
                  case 'file_saved':
                    callbacks.onFileSaved?.(event.filename || '', event.file_index || 0, event.total_files || 0, event.dataset)
                    break
                  case 'file_error':
                    callbacks.onFileError?.(event.filename || '', event.file_index || 0, event.total_files || 0, event.error || 'Unknown error')
                    break
                  case 'processing_started':
                    callbacks.onProcessingStarted?.(event.message || '')
                    break
                  case 'complete':
                    callbacks.onComplete?.(event.success_count || 0, event.fail_count || 0, event.total_files || 0, event.message || '')
                    break
                }
              } catch (e) {
                console.error('Failed to parse SSE event:', line, e)
              }
            }
          }
        }
      })
      .catch((error) => {
        if (error.name !== 'AbortError') {
          callbacks.onError?.(error)
        }
      })
    
    return controller
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
