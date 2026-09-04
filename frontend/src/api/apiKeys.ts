import { fetchAPI } from '@/lib/api-client'
import type {
  APIResponse,
  APIKeysResponse,
  CreateAPIKeyRequest,
  CreateAPIKeyResponse,
} from '@/types/api'

export const apiKeysAPI = {
  list: async (): Promise<APIResponse<APIKeysResponse>> => {
    return fetchAPI('/api/v1/user/api-keys')
  },

  create: async (
    data: CreateAPIKeyRequest
  ): Promise<APIResponse<CreateAPIKeyResponse>> => {
    return fetchAPI('/api/v1/user/api-keys', {
      method: 'POST',
      body: JSON.stringify(data),
    })
  },

  /** Replaces the secret behind an existing key; the key keeps its id, name,
   *  scopes and usage history. The old secret stops working immediately. */
  roll: async (keyId: string): Promise<APIResponse<CreateAPIKeyResponse>> => {
    return fetchAPI(`/api/v1/user/api-keys/${keyId}/roll`, {
      method: 'POST',
    })
  },

  delete: async (keyId: string): Promise<APIResponse<null>> => {
    return fetchAPI(`/api/v1/user/api-keys/${keyId}`, {
      method: 'DELETE',
    })
  },
}
