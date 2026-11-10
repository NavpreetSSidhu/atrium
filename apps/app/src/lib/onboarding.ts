import type { Tenant } from './api.ts'

export type Progress = { services: number; hasHours: boolean }
export type Step = { title: string; body: string; done?: boolean; to?: string }

// Getting-started walkthrough, tailored to what the space offers. Steps link to their page once the
// module exists; the rest say "Coming soon" (PRD §6).
export function checklist(t: Tenant, p: Progress): Step[] {
  return [
    { title: 'Create your space', body: `${t.slug}.atrium.in is reserved for you.`, done: true },
    ...(t.focus.includes('appointments')
      ? [
          { title: 'Add a service', body: 'Set the length, fee and whether it is video, in person or phone.', done: p.services > 0, to: '/services' },
          { title: 'Set your availability', body: 'Weekly hours, buffers between bookings and time off.', done: p.hasHours, to: '/availability' },
          { title: 'Share your booking page', body: 'Send clients your link so they can book themselves.', to: `/book/${t.slug}` },
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
