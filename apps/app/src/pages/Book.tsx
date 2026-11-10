import { useQuery, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { getRouteApi, Link } from '@tanstack/react-router'
import { useMemo, useState } from 'react'
import { Button, Field, Input, Logo } from '../components/ui'
import { ApiError, book, meQuery, providersQuery, servicesQuery, setName, slotsQuery, type Booking, type Service } from '../lib/api'
import { localDate, rupees } from '../lib/format'

const route = getRouteApi('/signed-in/book/$slug')
const DAYS = 14
const locations = { video: 'Video call', in_person: 'In person', phone: 'Phone call' }
const timeFmt = new Intl.DateTimeFormat(undefined, { hour: 'numeric', minute: '2-digit' })
const browserTz = Intl.DateTimeFormat().resolvedOptions().timeZone

export function BookPage() {
  const tenant = route.useLoaderData()
  const { data: services } = useSuspenseQuery(servicesQuery())
  const { data: providers } = useSuspenseQuery(providersQuery())
  const [service, setService] = useState<Service | null>(services.length === 1 ? services[0]! : null)
  const [providerId, setProviderId] = useState(providers.length === 1 ? providers[0]!.id : '')
  const [booked, setBooked] = useState<Booking>()
  const provider = providers.find((p) => p.id === providerId)

  return (
    <div className="min-h-svh bg-zinc-50 px-4 py-8">
      <div className="mx-auto max-w-2xl">
        <div className="mb-6 flex items-center justify-between">
          <Logo />
          <Link to="/" className="text-sm font-medium text-zinc-600 hover:text-zinc-900">
            My appointments
          </Link>
        </div>
        <main className="rounded-2xl border border-zinc-200 bg-white p-5 shadow-sm sm:p-8">
          <p className="text-sm font-medium text-indigo-600">{tenant.name}</p>
          {booked ? (
            <Done booking={booked} />
          ) : !services.length || !providers.length ? (
            <p className="mt-2 text-zinc-600">{tenant.name} isn't taking online bookings yet. Please check back soon.</p>
          ) : !service ? (
            <>
              <h1 className="mt-1 text-2xl font-semibold text-zinc-900">Book an appointment</h1>
              <ul className="mt-6 space-y-3">
                {services.map((s) => (
                  <li key={s.id}>
                    <button
                      onClick={() => setService(s)}
                      className="w-full rounded-xl border border-zinc-200 p-4 text-left transition-colors hover:border-indigo-600 hover:bg-indigo-50/50 focus-visible:outline-2 focus-visible:outline-indigo-600"
                    >
                      <span className="flex justify-between gap-3 font-medium text-zinc-900">
                        {s.title}
                        <span>{rupees(s.price_paise)}</span>
                      </span>
                      <span className="mt-1 block text-sm text-zinc-600">
                        {s.duration_min} min · {locations[s.location]}
                      </span>
                      {s.description && <span className="mt-1 block text-sm text-zinc-500">{s.description}</span>}
                    </button>
                  </li>
                ))}
              </ul>
            </>
          ) : !provider ? (
            <>
              <BackTo onClick={() => setService(null)} label="Services" />
              <h1 className="text-2xl font-semibold text-zinc-900">Who would you like to see?</h1>
              <ul className="mt-6 space-y-3">
                {providers.map((p) => (
                  <li key={p.id}>
                    <button
                      onClick={() => setProviderId(p.id)}
                      className="w-full rounded-xl border border-zinc-200 p-4 text-left font-medium text-zinc-900 hover:border-indigo-600 focus-visible:outline-2 focus-visible:outline-indigo-600"
                    >
                      {p.title || 'Provider'}
                    </button>
                  </li>
                ))}
              </ul>
            </>
          ) : (
            <>
              {(services.length > 1 || providers.length > 1) && (
                <BackTo onClick={() => (providers.length > 1 ? setProviderId('') : setService(null))} label="Back" />
              )}
              <h1 className="text-2xl font-semibold text-zinc-900">{service.title}</h1>
              <p className="mt-1 text-zinc-600">
                {service.duration_min} min · {locations[service.location]} · {rupees(service.price_paise)}
                {provider.title && ` · ${provider.title}`}
              </p>
              <PickTime service={service} providerId={provider.id} onBooked={setBooked} />
            </>
          )}
        </main>
      </div>
    </div>
  )
}

function BackTo({ onClick, label }: { onClick: () => void; label: string }) {
  return (
    <button onClick={onClick} className="mb-3 text-sm font-medium text-zinc-500 hover:text-zinc-900">
      ← {label}
    </button>
  )
}

function PickTime({ service, providerId, onBooked }: { service: Service; providerId: string; onBooked: (b: Booking) => void }) {
  const qc = useQueryClient()
  const [days] = useState(() => {
    const d = new Date()
    return Array.from({ length: DAYS }, (_, i) => new Date(d.getFullYear(), d.getMonth(), d.getDate() + i))
  })
  const dates = days.map(localDate)
  const q = slotsQuery(service.id, providerId, dates[0]!, dates.at(-1)!)
  const { data: slots, isPending, isError } = useQuery(q)
  const byDate = useMemo(() => {
    const m = new Map<string, string[]>()
    for (const s of slots ?? []) {
      const k = localDate(new Date(s))
      m.set(k, [...(m.get(k) ?? []), s])
    }
    return m
  }, [slots])
  const [date, setDate] = useState<string>()
  const [slot, setSlot] = useState<string>()
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)
  const { data: me } = useSuspenseQuery(meQuery)
  const [name, setNameInput] = useState(me.name)
  const day = date ?? dates.find((d) => byDate.has(d))

  async function confirm() {
    setBusy(true)
    setError(undefined)
    try {
      // The clinic needs to know who is coming; ask once, on the first booking.
      if (!me.name) qc.setQueryData(meQuery.queryKey, await setName(name))
      onBooked(await book({ service_id: service.id, provider_id: providerId, starts_at: slot! }))
      qc.invalidateQueries()
    } catch (e) {
      setError(e instanceof ApiError && e.status === 409 ? 'Someone just took that time. Please pick another.' : 'Booking failed. Please try again.')
      setSlot(undefined)
      qc.invalidateQueries({ queryKey: q.queryKey })
    } finally {
      setBusy(false)
    }
  }

  if (isPending) return <p className="mt-8 text-zinc-500">Finding open times…</p>
  if (isError) return <p className="mt-8 text-red-600">Couldn't load open times. Refresh to try again.</p>
  if (!slots.length) return <p className="mt-8 text-zinc-600">No open times in the next two weeks. Please check back later.</p>

  return (
    <div className="mt-6">
      <h2 className="text-sm font-semibold text-zinc-900">Pick a day</h2>
      <div className="-mx-1 mt-2 flex gap-2 overflow-x-auto px-1 pb-2" role="listbox" aria-label="Days">
        {dates.map((d, i) => {
          const open = byDate.get(d)?.length ?? 0
          const dt = days[i]!
          return (
            <button
              key={d}
              role="option"
              aria-selected={d === day}
              disabled={!open}
              onClick={() => {
                setDate(d)
                setSlot(undefined)
              }}
              className={`flex w-16 shrink-0 flex-col items-center rounded-xl border py-2 text-sm transition-colors disabled:cursor-not-allowed disabled:opacity-40 ${d === day ? 'border-indigo-600 bg-indigo-600 text-white' : 'border-zinc-200 text-zinc-900 hover:border-indigo-600'}`}
            >
              <span className="text-xs">{dt.toLocaleDateString(undefined, { weekday: 'short' })}</span>
              <span className="text-lg font-semibold">{dt.getDate()}</span>
            </button>
          )
        })}
      </div>

      <h2 className="mt-6 text-sm font-semibold text-zinc-900">Pick a time</h2>
      <div className="mt-2 grid grid-cols-3 gap-2 sm:grid-cols-4">
        {(byDate.get(day!) ?? []).map((s) => (
          <button
            key={s}
            aria-pressed={s === slot}
            onClick={() => setSlot(s)}
            className={`h-11 rounded-lg border text-sm font-medium tabular-nums transition-colors ${s === slot ? 'border-indigo-600 bg-indigo-600 text-white' : 'border-zinc-200 text-zinc-900 hover:border-indigo-600'}`}
          >
            {timeFmt.format(new Date(s))}
          </button>
        ))}
      </div>
      <p className="mt-3 text-xs text-zinc-500">Times are shown in your timezone ({browserTz}).</p>

      {error && (
        <p role="alert" className="mt-4 text-sm text-red-600">
          {error}
        </p>
      )}
      {!me.name && slot && (
        <div className="mt-6">
          <Field label="Your name" hint="So they know who to expect.">
            <Input autoComplete="name" value={name} onChange={(e) => setNameInput(e.target.value)} />
          </Field>
        </div>
      )}
      <Button className="mt-6 w-full" disabled={!slot || busy || (!me.name && !name.trim())} onClick={confirm}>
        {busy ? 'Booking…' : slot ? `Book ${new Date(slot).toLocaleDateString(undefined, { day: 'numeric', month: 'short' })}, ${timeFmt.format(new Date(slot))}` : 'Pick a time'}
      </Button>
    </div>
  )
}

function Done({ booking }: { booking: Booking }) {
  const when = new Date(booking.starts_at).toLocaleString(undefined, { weekday: 'long', day: 'numeric', month: 'long', hour: 'numeric', minute: '2-digit' })
  const held = booking.status === 'held'
  return (
    <div className="py-4 text-center">
      <div className={`mx-auto grid size-12 place-items-center rounded-full text-2xl ${held ? 'bg-amber-100 text-amber-700' : 'bg-emerald-100 text-emerald-700'}`} aria-hidden>
        {held ? '⏳' : '✓'}
      </div>
      <h1 className="mt-4 text-2xl font-semibold text-zinc-900">{held ? 'Your time is held' : "You're booked"}</h1>
      <p className="mt-2 text-zinc-600">
        {booking.service_title} with {booking.provider_title} on {when}.
      </p>
      {held && (
        <p className="mt-2 text-sm text-amber-700">
          This is a paid appointment. Online payment isn't available yet, so the hold ends in 10 minutes. Please contact the clinic to confirm.
        </p>
      )}
      <Link to="/" className="mt-6 inline-block text-sm font-medium text-indigo-600 hover:text-indigo-500">
        See my appointments →
      </Link>
    </div>
  )
}
