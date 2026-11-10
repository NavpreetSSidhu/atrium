import type { Tenant } from './api.ts'

const TENANT_KEY = 'atrium.tenant'

// Remembers the selected space per browser. Storage can be blocked, so failures are ignored.
export function selectTenant(id: string) {
  try {
    localStorage.setItem(TENANT_KEY, id)
  } catch {}
}
export function savedTenant() {
  try {
    return localStorage.getItem(TENANT_KEY)
  } catch {
    return null
  }
}

type Step = { title: string; body: string; done?: boolean }

// Getting-started walkthrough, tailored to what the space offers. Steps unlock as their modules ship (PRD §6).
export function checklist(t: Tenant): Step[] {
  return [
    { title: 'Create your space', body: `${t.slug}.atrium.in is reserved for you.`, done: true },
    ...(t.focus.includes('appointments')
      ? [
          { title: 'Add a service', body: 'Set the length, fee and whether it is video, in person or phone.' },
          { title: 'Set your availability', body: 'Weekly hours, buffers between bookings and time off.' },
        ]
      : []),
    ...(t.focus.includes('courses')
      ? [
          { title: 'Create your first course', body: 'Upload lessons, set a price and a drip schedule.' },
          { title: 'Schedule a live class', body: 'Teach up to 100 people live, with recording.' },
        ]
      : []),
    { title: 'Design your site', body: 'Pick a theme, add pages and publish your legal pages.' },
    { title: 'Connect payouts', body: 'Link your bank through Razorpay to get paid directly.' },
  ]
}
