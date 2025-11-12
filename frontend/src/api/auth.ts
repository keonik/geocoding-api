import { fetchAPI } from '@/lib/api-client'
import type {
  APIResponse,
  AuthResponse,
  LoginRequest,
  RegisterRequest,
  User,
} from '@/types/api'

export const authAPI = {
  login: async (data: LoginRequest): Promise<APIResponse<AuthResponse>> => {
    return fetchAPI('/api/v1/auth/login', {
      method: 'POST',
      body: JSON.stringify(data),
    })
  },

  register: async (data: RegisterRequest): Promise<APIResponse<AuthResponse>> => {
    return fetchAPI('/api/v1/auth/register', {
      method: 'POST',
      body: JSON.stringify(data),
    })
  },

  getProfile: async (): Promise<APIResponse<User>> => {
    return fetchAPI('/api/v1/user/profile')
  },

  logout: () => {
    localStorage.removeItem('authToken')
    localStorage.removeItem('user')
  },
}
