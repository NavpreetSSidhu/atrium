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

// The selected space, sent as X-Tenant-ID. Remembered per browser; storage can be blocked, so it
// also lives in memory.
const TENANT_KEY = 'atrium.tenant'
let tenantId: string | null = null
export function selectTenant(id: string) {
  tenantId = id
  try {
    localStorage.setItem(TENANT_KEY, id)
  } catch {}
}
export function currentTenant() {
  if (!tenantId) {
    try {
      tenantId = localStorage.getItem(TENANT_KEY)
    } catch {}
  }
  return tenantId
}

// The access token lives only in memory. The refresh token is an httpOnly cookie JS can't read.
let accessToken: string | null = null

function send(path: string, init: RequestInit = {}) {
  const headers = new Headers(init.headers)
  if (init.body) headers.set('Content-Type', 'application/json')
  if (accessToken) headers.set('Authorization', `Bearer ${accessToken}`)
  if (currentTenant()) headers.set('X-Tenant-ID', currentTenant()!)
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

export const joinTenant = (slug: string) => api<Tenant>('/tenants/join', post({ slug }))

export type Location = 'video' | 'in_person' | 'phone'
export type Service = {
  id: string
  title: string
  description: string
  price_paise: number
  status: 'draft' | 'published' | 'archived'
  duration_min: number
  buffer_min: number
  location: Location
  min_notice_min: number
  horizon_days: number
  cancel_cutoff_hours: number
}
export type Provider = { id: string; title: string; timezone: string; me: boolean }
export type Hours = { start_min: number; end_min: number }
export type Rule = Hours & { weekday: number }
export type Override = { date: string; start_min: number | null; end_min: number | null }
export type Availability = { rules: Rule[]; overrides: Override[] }
export type Booking = {
  id: string
  service_id: string | null
  service_title: string
  provider_id: string
  provider_title: string
  customer_name: string
  mine: boolean
  starts_at: string
  ends_at: string
  status: 'held' | 'confirmed' | 'completed' | 'cancelled'
  hold_expires_at: string | null
}

export const saveService = (s: Omit<Service, 'id'> & { id?: string }) =>
  s.id ? api<Service>(`/services/${s.id}`, post(s, 'PUT')) : api<Service>('/services', post(s))
export const becomeProvider = (title: string, timezone: string) => api<Provider>('/providers/me', post({ title, timezone }, 'PUT'))
export const saveAvailability = (providerId: string, a: Availability) =>
  api<Availability>(`/providers/${providerId}/availability`, post(a, 'PUT'))
export const book = (b: { service_id: string; provider_id: string; starts_at: string }) => api<Booking>('/bookings', post(b))
export const cancelBooking = (id: string) => api<void>(`/bookings/${id}/cancel`, post())

// Query keys include the space so switching spaces never shows another space's data.
const scoped = (...key: string[]) => [currentTenant() ?? '', ...key]
export const servicesQuery = () => queryOptions({ queryKey: scoped('services'), queryFn: () => api<Service[]>('/services') })
export const providersQuery = () => queryOptions({ queryKey: scoped('providers'), queryFn: () => api<Provider[]>('/providers') })
export const availabilityQuery = (providerId: string) =>
  queryOptions({ queryKey: scoped('availability', providerId), queryFn: () => api<Availability>(`/providers/${providerId}/availability`) })
export const slotsQuery = (serviceId: string, providerId: string, from: string, to: string) =>
  queryOptions({
    queryKey: scoped('slots', serviceId, providerId, from, to),
    queryFn: () => api<string[]>(`/slots?${new URLSearchParams({ service_id: serviceId, provider_id: providerId, from, to })}`),
  })
export const bookingsQuery = (from: Date, to: Date) =>
  queryOptions({
    queryKey: scoped('bookings', from.toISOString(), to.toISOString()),
    queryFn: () => api<Booking[]>(`/bookings?${new URLSearchParams({ from: from.toISOString(), to: to.toISOString() })}`),
  })

export const meQuery = queryOptions({ queryKey: ['me'], queryFn: () => api<Me>('/me') })
export const tenantsQuery = queryOptions({ queryKey: ['tenants'], queryFn: () => api<Tenant[]>('/tenants') })
