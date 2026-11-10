// Accepts "98765 43210", "+91 98765-43210" or a full E.164 number; bare 10 digits default to India.
export function toE164(s: string) {
  const d = s.replace(/[\s()-]/g, '')
  return d.startsWith('+') ? d : d.length === 10 ? `+91${d}` : `+${d}`
}

export function slugify(s: string) {
  return s
    .toLowerCase()
    .normalize('NFKD')
    .replace(/[^a-z0-9\s-]/g, '')
    .trim()
    .replace(/[\s-]+/g, '-')
    .slice(0, 40)
    .replace(/-$/, '')
}

// ₹500 for whole rupees, ₹1,499.50 otherwise (never ₹1,499.5).
export const rupees = (paise: number) =>
  paise === 0
    ? 'Free'
    : new Intl.NumberFormat('en-IN', { style: 'currency', currency: 'INR', minimumFractionDigits: paise % 100 ? 2 : 0 }).format(paise / 100)

// Minutes from midnight <-> "HH:MM" for <input type="time">. 1440 (end of day) shows as 23:59.
export const toHHMM = (min: number) => {
  const m = Math.min(min, 1439)
  return `${String(Math.floor(m / 60)).padStart(2, '0')}:${String(m % 60).padStart(2, '0')}`
}
export const fromHHMM = (s: string) => {
  const [h, m] = s.split(':').map(Number)
  return h! * 60 + m!
}

// Local calendar date (YYYY-MM-DD) of a Date, in the browser's timezone.
export const localDate = (d: Date) =>
  `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`

// Only same-site paths may be used as a post-login redirect (no open redirects).
export const safeRedirect = (to: unknown) => (typeof to === 'string' && to.startsWith('/') && !to.startsWith('//') && !to.startsWith('/\\') ? to : '/')

// Today 00:00 to +60 days: a stable range, so the query key doesn't change on every render.
export function upcoming() {
  const from = new Date()
  from.setHours(0, 0, 0, 0)
  const to = new Date(from)
  to.setDate(to.getDate() + 60)
  return [from, to] as const
}
