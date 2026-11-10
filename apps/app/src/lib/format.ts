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
