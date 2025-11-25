const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || ''

export class APIError extends Error {
  constructor(
    public status: number,
    public message: string,
    public data?: unknown
  ) {
    super(message)
    this.name = 'APIError'
  }
}

// Handle authentication errors globally
function handleAuthError(error: APIError) {
  if (error.status === 401) {
    // Token is expired or invalid, clear it and redirect to login
    localStorage.removeItem('authToken')
    window.location.href = '/auth/signin'
  }
}

export async function fetchAPI<T>(
  endpoint: string,
  options: RequestInit = {}
): Promise<T> {
  const token = localStorage.getItem('authToken')
  
  const headers: Record<string, string> = {}

  // Only set Content-Type for non-FormData requests
  // FormData needs browser to set the boundary automatically
  if (!(options.body instanceof FormData)) {
    headers['Content-Type'] = 'application/json'
  }

  // Merge existing headers (but skip empty header objects for FormData)
  if (options.headers && Object.keys(options.headers).length > 0) {
    const existingHeaders = new Headers(options.headers)
    existingHeaders.forEach((value, key) => {
      headers[key] = value
    })
  }

  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }

  let response: Response
  try {
    response = await fetch(`${API_BASE_URL}${endpoint}`, {
      ...options,
      headers,
    })
  } catch (fetchError) {
    console.error('[API] Fetch error:', fetchError)
    throw new APIError(0, 'Network error: Could not connect to server')
  }

  // Check if response has content
  const contentType = response.headers.get('content-type')
  const contentLength = response.headers.get('content-length')
  
  let data: T
  
  // Try to parse JSON response
  if (contentType?.includes('application/json') || contentLength !== '0') {
    try {
      const text = await response.text()
      if (text.trim() === '') {
        console.error('[API] Empty response from server')
        throw new APIError(response.status, 'Server returned an empty response')
      }
      data = JSON.parse(text)
    } catch (parseError) {
      console.error('[API] JSON parse error:', parseError)
      throw new APIError(
        response.status,
        `Failed to parse server response: ${parseError instanceof Error ? parseError.message : 'Unknown error'}`
      )
    }
  } else {
    // No content
    if (!response.ok) {
      throw new APIError(response.status, `Request failed with status ${response.status}`)
    }
    data = {} as T
  }

  if (!response.ok) {
    const apiError = new APIError(
      response.status,
      (data as { error?: string }).error || 'An error occurred',
      data
    )
    
    // Handle auth errors globally
    handleAuthError(apiError)
    
    throw apiError
  }

  return data
}
