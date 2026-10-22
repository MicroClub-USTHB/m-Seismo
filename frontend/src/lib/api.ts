// Thin fetch wrapper for the m-Seismo backend (backend/docs/gitlab-bitbucket.md,
// backend/bruno). Every request/response body is JSON; errors are always
// `{"error": "..."}` (backend/bruno/collection.bru).
const BASE_URL = import.meta.env.VITE_API_BASE_URL ?? 'http://localhost:8080/api/v1'

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

let authToken: string | null = null

/** Sets the bearer token every subsequent request carries. */
export function setAuthToken(token: string | null) {
  authToken = token
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers)
  headers.set('Accept', 'application/json')
  if (init?.body) headers.set('Content-Type', 'application/json')
  if (authToken) headers.set('Authorization', `Bearer ${authToken}`)

  const res = await fetch(`${BASE_URL}${path}`, { ...init, headers })
  if (res.status === 204) return undefined as T

  const text = await res.text()
  const data = text ? JSON.parse(text) : null

  if (!res.ok) {
    throw new ApiError(res.status, (data && data.error) || `request failed with status ${res.status}`)
  }
  return data as T
}

export const api = {
  get: <T>(path: string) => request<T>(path),
  post: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: 'POST', body: body !== undefined ? JSON.stringify(body) : undefined }),
  patch: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: 'PATCH', body: body !== undefined ? JSON.stringify(body) : undefined }),
  put: <T>(path: string, body?: unknown) =>
    request<T>(path, { method: 'PUT', body: body !== undefined ? JSON.stringify(body) : undefined }),
  delete: <T>(path: string) => request<T>(path, { method: 'DELETE' }),
}

export interface Owner {
  id: string
  email: string
  name: string
  created_at: string
  updated_at: string
}

export interface Token {
  access_token: string
  token_type: string
  expires_at: string
}

export interface SessionResponse {
  owner: Owner
  token: Token
}

export type Tracker = 'github' | 'gitlab' | 'bitbucket'
export type Platform = 'ios' | 'android' | 'flutter' | 'react_native'

export interface App {
  id: string
  name: string
  platform: Platform
  bundle_id: string
  created_at: string
  updated_at: string
}

export interface Connection {
  id: string
  app_id: string
  tracker: Tracker
  account_login: string
  account_type: string
  suspended_at: string | null
  created_at: string
  updated_at: string
}

export interface ConnectionRepository {
  full_name: string
  private: boolean
  url: string
}

export interface CredentialStatus {
  tracker: Tracker
  configured: boolean
  client_id?: string
}

export interface Project {
  id: string
  name: string
  slug: string
  created_at: string
  updated_at: string
}

export interface AppProjectLink {
  id: string
  name: string
  slug: string
}

export interface ProjectRepository {
  id: string
  project_id: string
  tracker: Tracker
  repository: string
  is_default: boolean
  created_at: string
}

export interface AppKey {
  id: string
  prefix: string
  revoked_at: string | null
  created_at: string
}

export interface AppKeyCreated extends AppKey {
  secret: string
}

export interface Target {
  project_id: string
  project_repository_id: string
  repository: string
  tracker: Tracker
  connection_id: string | null
  account_login?: string
  error?: string
}

export const ALL_TRACKERS: Tracker[] = ['github', 'gitlab', 'bitbucket']
