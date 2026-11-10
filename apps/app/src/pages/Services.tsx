import { useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { Button, Field, Input } from '../components/ui'
import { saveService, servicesQuery, type Location, type Service } from '../lib/api'
import { rupees } from '../lib/format'

const locations: Record<Location, string> = { video: 'Video call', in_person: 'In person', phone: 'Phone call' }
const blank: Omit<Service, 'id'> = {
  title: '',
  description: '',
  price_paise: 0,
  status: 'published',
  duration_min: 30,
  buffer_min: 10,
  location: 'video',
  min_notice_min: 120,
  horizon_days: 60,
  cancel_cutoff_hours: 24,
}

export function ServicesPage() {
  const { data: services } = useSuspenseQuery(servicesQuery())
  const [editing, setEditing] = useState<(Omit<Service, 'id'> & { id?: string }) | null>(services.length ? null : blank)

  return (
    <>
      <div className="mb-6 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold text-zinc-900">Services</h1>
          <p className="mt-1 text-zinc-600">What clients can book: consultations, sessions, check-ups.</p>
        </div>
        {!editing && <Button onClick={() => setEditing(blank)}>New service</Button>}
      </div>
      {editing && <ServiceForm key={editing.id ?? 'new'} initial={editing} onDone={() => setEditing(null)} canCancel={services.length > 0} />}
      <ul className="mt-6 grid gap-3 sm:grid-cols-2">
        {services.map((s) => (
          <li key={s.id} className="flex flex-col rounded-2xl border border-zinc-200 bg-white p-5">
            <div className="flex items-start justify-between gap-3">
              <h2 className="font-semibold text-zinc-900">{s.title}</h2>
              <span className="shrink-0 font-medium text-zinc-900">{rupees(s.price_paise)}</span>
            </div>
            <p className="mt-1 text-sm text-zinc-600">
              {s.duration_min} min · {locations[s.location]}
              {s.status === 'draft' && <span className="ml-2 rounded-full bg-zinc-100 px-2 py-0.5 text-xs">Draft</span>}
            </p>
            {s.description && <p className="mt-2 line-clamp-2 text-sm text-zinc-600">{s.description}</p>}
            <Button variant="ghost" className="mt-3 -ml-3 h-9 self-start" onClick={() => setEditing(s)}>
              Edit
            </Button>
          </li>
        ))}
      </ul>
    </>
  )
}

function ServiceForm({ initial, onDone, canCancel }: { initial: Omit<Service, 'id'> & { id?: string }; onDone: () => void; canCancel: boolean }) {
  const qc = useQueryClient()
  const [v, setV] = useState(initial)
  const [price, setPrice] = useState(initial.price_paise ? String(initial.price_paise / 100) : '')
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)
  const set = <K extends keyof typeof v>(k: K, val: (typeof v)[K]) => setV({ ...v, [k]: val })
  const num = (s: string) => (s === '' ? 0 : Number(s))

  async function submit(e: FormEvent, status = v.status) {
    e.preventDefault()
    const paise = Math.round(num(price) * 100)
    if (!Number.isFinite(paise) || paise < 0) return setError('Enter a valid price.')
    setBusy(true)
    try {
      await saveService({ ...v, price_paise: paise, status })
      await qc.invalidateQueries()
      onDone()
    } catch {
      setError('Check the fields: a title, 5–480 minute duration and sensible limits.')
    } finally {
      setBusy(false)
    }
  }

  const select = 'h-11 w-full rounded-lg border border-zinc-300 bg-white px-3 text-zinc-900 focus:border-indigo-600 focus:outline-none focus:ring-2 focus:ring-indigo-600/20'
  return (
    <form onSubmit={submit} className="rounded-2xl border border-zinc-200 bg-white p-5 sm:p-6">
      <h2 className="font-semibold text-zinc-900">{initial.id ? 'Edit service' : 'New service'}</h2>
      <div className="mt-4 grid gap-4 sm:grid-cols-2">
        <div className="sm:col-span-2">
          <Field label="Name">
            <Input required autoFocus placeholder="Skin consultation" value={v.title} onChange={(e) => set('title', e.target.value)} />
          </Field>
        </div>
        <div className="sm:col-span-2">
          <Field label="Description" hint="Optional. Shown on your booking page.">
            <Input value={v.description} onChange={(e) => set('description', e.target.value)} />
          </Field>
        </div>
        <Field label="Length">
          <select className={select} value={v.duration_min} onChange={(e) => set('duration_min', num(e.target.value))}>
            {[15, 20, 30, 45, 60, 90, 120].map((m) => (
              <option key={m} value={m}>
                {m} minutes
              </option>
            ))}
          </select>
        </Field>
        <Field label="Fee (₹)" hint="Leave empty for free.">
          <Input inputMode="decimal" placeholder="0" value={price} onChange={(e) => setPrice(e.target.value.replace(/[^\d.]/g, ''))} />
        </Field>
        <Field label="Where">
          <select className={select} value={v.location} onChange={(e) => set('location', e.target.value as Location)}>
            {Object.entries(locations).map(([k, label]) => (
              <option key={k} value={k}>
                {label}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Break after each booking">
          <select className={select} value={v.buffer_min} onChange={(e) => set('buffer_min', num(e.target.value))}>
            {[0, 5, 10, 15, 30].map((m) => (
              <option key={m} value={m}>
                {m ? `${m} minutes` : 'None'}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Minimum notice">
          <select className={select} value={v.min_notice_min} onChange={(e) => set('min_notice_min', num(e.target.value))}>
            {[0, 60, 120, 240, 720, 1440, 2880].map((m) => (
              <option key={m} value={m}>
                {m === 0 ? 'None' : m < 1440 ? `${m / 60} hours` : `${m / 1440} days`}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Clients can cancel up to">
          <select className={select} value={v.cancel_cutoff_hours} onChange={(e) => set('cancel_cutoff_hours', num(e.target.value))}>
            {[0, 2, 6, 12, 24, 48].map((h) => (
              <option key={h} value={h}>
                {h ? `${h} hours before` : 'Any time'}
              </option>
            ))}
          </select>
        </Field>
      </div>
      {error && (
        <p role="alert" className="mt-4 text-sm text-red-600">
          {error}
        </p>
      )}
      <div className="mt-6 flex flex-wrap gap-3">
        <Button type="submit" disabled={busy || !v.title.trim()}>
          {busy ? 'Saving…' : 'Save'}
        </Button>
        {canCancel && (
          <Button type="button" variant="ghost" onClick={onDone}>
            Cancel
          </Button>
        )}
        {initial.id && (
          <Button type="button" variant="ghost" className="ml-auto text-red-600 hover:bg-red-50 hover:text-red-700" disabled={busy} onClick={(e) => submit(e, 'archived')}>
            Archive
          </Button>
        )}
      </div>
    </form>
  )
}
