import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState, type ReactNode } from 'react'
import { ApiError, cancelBooking, type Booking } from '../lib/api'
import { Button } from './ui'

const day = new Intl.DateTimeFormat(undefined, { weekday: 'long', day: 'numeric', month: 'long' })
const time = new Intl.DateTimeFormat(undefined, { hour: 'numeric', minute: '2-digit' })

// Upcoming bookings grouped by day, with cancel. `perspective` decides whose name to show.
export function BookingList({ bookings, perspective, empty }: { bookings: Booking[]; perspective: 'business' | 'customer'; empty: ReactNode }) {
  const qc = useQueryClient()
  const [error, setError] = useState<string>()
  const cancel = useMutation({
    mutationFn: cancelBooking,
    onSuccess: () => qc.invalidateQueries(),
    onError: (e) =>
      setError(e instanceof ApiError && e.status === 409 ? 'It is too close to the appointment to cancel online. Please contact them directly.' : 'Could not cancel. Try again.'),
  })

  if (!bookings.length) return <div className="rounded-2xl border border-dashed border-zinc-300 bg-white p-8 text-center text-zinc-600">{empty}</div>

  const groups = new Map<string, Booking[]>()
  for (const b of bookings) {
    const k = day.format(new Date(b.starts_at))
    groups.set(k, [...(groups.get(k) ?? []), b])
  }
  return (
    <div className="space-y-6">
      {error && (
        <p role="alert" className="rounded-lg bg-red-50 p-3 text-sm text-red-700">
          {error}
        </p>
      )}
      {[...groups].map(([label, list]) => (
        <section key={label}>
          <h3 className="mb-2 text-sm font-semibold text-zinc-500">{label}</h3>
          <ul className="divide-y divide-zinc-100 rounded-2xl border border-zinc-200 bg-white">
            {list.map((b) => (
              <li key={b.id} className="flex flex-wrap items-center gap-x-4 gap-y-2 p-4">
                <span className="w-20 shrink-0 font-medium text-zinc-900 tabular-nums">{time.format(new Date(b.starts_at))}</span>
                <div className="min-w-0 flex-1">
                  <p className="truncate font-medium text-zinc-900">{b.service_title || 'Appointment'}</p>
                  <p className="truncate text-sm text-zinc-600">
                    {perspective === 'business' ? b.customer_name || 'Customer' : `with ${b.provider_title || 'your provider'}`}
                  </p>
                </div>
                {b.status === 'held' && (
                  <span className="rounded-full bg-amber-100 px-2.5 py-1 text-xs font-medium text-amber-800">Awaiting payment</span>
                )}
                <Button
                  variant="ghost"
                  className="h-9"
                  disabled={cancel.isPending}
                  onClick={() => {
                    setError(undefined)
                    if (confirm('Cancel this appointment?')) cancel.mutate(b.id)
                  }}
                >
                  Cancel
                </Button>
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  )
}
