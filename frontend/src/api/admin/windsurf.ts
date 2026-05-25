import { apiClient } from '../client'

export interface WindsurfCredentials {
  api_key?: string
  auth_method?: string
  email?: string
  account_id?: string
  plan_name?: string
  name?: string
  updated_at?: number
  [key: string]: unknown
}

export interface WindsurfOAuthURLResponse {
  authorize_url: string
  state: string
}

export async function importAPIKey(payload: {
  name?: string
  api_key: string
}): Promise<WindsurfCredentials> {
  const { data } = await apiClient.post<WindsurfCredentials>('/admin/windsurf/import-api-key', payload)
  return data
}

export async function loginWithPassword(payload: {
  name?: string
  email: string
  password: string
  proxy_url?: string
}): Promise<WindsurfCredentials> {
  const { data } = await apiClient.post<WindsurfCredentials>('/admin/windsurf/login-password', payload)
  return data
}

export async function importToken(payload: {
  name?: string
  token: string
  proxy_url?: string
}): Promise<WindsurfCredentials> {
  const { data } = await apiClient.post<WindsurfCredentials>('/admin/windsurf/import-token', payload)
  return data
}

export async function generateOAuthURL(payload: {
  state: string
}): Promise<WindsurfOAuthURLResponse> {
  const { data } = await apiClient.post<WindsurfOAuthURLResponse>('/admin/windsurf/oauth/auth-url', payload)
  return data
}

export default {
  importAPIKey,
  loginWithPassword,
  importToken,
  generateOAuthURL,
}
