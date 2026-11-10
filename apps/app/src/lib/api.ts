// shortcut: hand-written client until packages/api-client is generated from the OpenAPI spec; replace it then.
import { queryOptions } from '@tanstack/react-query'

export type Me = { id: string; phone?: string; email?: string; name: string }
export type Focus = 'courses' | 'appointments'
export type Tenant = { id: string; slug: string; name: string; role: string; focus: Focus[] }

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

// The access token lives only in memory. The refresh token is an httpOnly cookie JS can't read.
let accessToken: string | null = null

function send(path: string, init: RequestInit = {}) {
  const headers = new Headers(init.headers)
  if (init.body) headers.set('Content-Type', 'application/json')
  if (accessToken) headers.set('Authorization', `Bearer ${accessToken}`)
  return fetch(`/api${path}`, { ...init, headers, credentials: 'same-origin' })
}

// Tabs share one refresh cookie. A Web Lock makes them rotate it one at a time, so two tabs
// refreshing together never looks like a stolen token (which revokes the session). See ADR-6.
export async function refresh(): Promise<boolean> {
  const stale = accessToken
  return await navigator.locks.request('atrium-refresh', async () => {
    if (accessToken !== stale) return accessToken !== null // this tab refreshed while we waited
    const res = await fetch('/api/auth/refresh', { method: 'POST', credentials: 'same-origin' })
    accessToken = res.ok ? ((await res.json()) as { access_token: string }).access_token : null
    return res.ok
  })
}

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  let res = await send(path, init)
  if (res.status === 401 && !path.startsWith('/auth/') && (await refresh())) res = await send(path, init)
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: string }
    throw new ApiError(res.status, body.error ?? res.statusText)
  }
  return (res.status === 204 ? undefined : await res.json()) as T
}

const post = (body?: unknown, method = 'POST'): RequestInit => ({ method, body: body === undefined ? undefined : JSON.stringify(body) })

export const auth = {
  signedIn: () => accessToken !== null || refresh(),
  start: (channel: 'sms' | 'email', dest: string) => api<void>('/auth/start', post({ channel, dest })),
  async verify(channel: 'sms' | 'email', dest: string, code: string) {
    accessToken = (await api<{ access_token: string }>('/auth/verify', post({ channel, dest, code }))).access_token
  },
  async logout() {
    await api('/auth/logout', post()).catch(() => {})
    accessToken = null
  },
}

export const setName = (name: string) => api<Me>('/me', post({ name }, 'PATCH'))
export const createTenant = (t: { name: string; slug: string; focus: Focus[] }) => api<Tenant>('/tenants', post(t))

export const meQuery = queryOptions({ queryKey: ['me'], queryFn: () => api<Me>('/me') })
export const tenantsQuery = queryOptions({ queryKey: ['tenants'], queryFn: () => api<Tenant[]>('/tenants') })
