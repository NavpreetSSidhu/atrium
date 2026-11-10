import { useQuery, useSuspenseQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { BookingList } from '../components/Bookings'
import { isStaff, useTenant } from '../lib/space'
import { availabilityQuery, bookingsQuery, meQuery, providersQuery, servicesQuery } from '../lib/api'
import { upcoming } from '../lib/format'
import { checklist } from '../lib/onboarding'

export function OverviewPage() {
  const { tenant } = useTenant()
  return isStaff(tenant) || tenant.role === 'provider' ? <Setup /> : <MyAppointments />
}

function MyAppointments() {
  const { tenant } = useTenant()
  const { data: bookings } = useSuspenseQuery(bookingsQuery(...upcoming()))
  return (
    <>
      <div className="mb-6 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold text-zinc-900">Your appointments</h1>
          <p className="mt-1 text-zinc-600">With {tenant.name}</p>
        </div>
        <Link to="/book/$slug" params={{ slug: tenant.slug }} className="text-sm font-medium text-indigo-600 hover:text-indigo-500">
          Book an appointment →
        </Link>
      </div>
      <BookingList bookings={bookings} perspective="customer" empty="No upcoming appointments." />
    </>
  )
}

function Setup() {
  const { tenant } = useTenant()
  const { data: me } = useSuspenseQuery(meQuery)
  const { data: services } = useSuspenseQuery(servicesQuery())
  const { data: providers } = useSuspenseQuery(providersQuery())
  const mine = providers.find((p) => p.me)
  const { data: hours } = useQuery({ ...availabilityQuery(mine?.id ?? ''), enabled: !!mine })
  const steps = checklist(tenant, { services: services.length, hasHours: !!hours?.rules.length })
  const done = steps.filter((s) => s.done).length

  return (
    <>
      <h1 className="text-2xl font-semibold text-zinc-900">Welcome, {me.name || 'there'}</h1>
      <p className="mt-1 text-zinc-600">Here's how to get {tenant.name} ready for your first customer.</p>

      <section className="mt-8 rounded-2xl border border-zinc-200 bg-white" aria-labelledby="getting-started">
        <div className="flex items-center justify-between gap-4 border-b border-zinc-200 p-5">
          <h2 id="getting-started" className="font-semibold text-zinc-900">
            Getting started
          </h2>
          <span className="text-sm text-zinc-500">
            {done} of {steps.length} done
          </span>
        </div>
        <div className="h-1 bg-zinc-100">
          <div className="h-1 bg-indigo-600 transition-all" style={{ width: `${(done / steps.length) * 100}%` }} />
        </div>
        <ol className="divide-y divide-zinc-100">
          {steps.map((s, i) => (
            <li key={s.title} className="flex items-start gap-4 p-5">
              <span
                className={`grid size-7 shrink-0 place-items-center rounded-full text-sm font-medium ${s.done ? 'bg-emerald-100 text-emerald-700' : 'border border-zinc-300 text-zinc-500'}`}
                aria-hidden
              >
                {s.done ? '✓' : i + 1}
              </span>
              <div className="min-w-0 flex-1">
                <p className={`font-medium ${s.done ? 'text-zinc-500 line-through' : 'text-zinc-900'}`}>{s.title}</p>
                <p className="mt-0.5 text-sm text-zinc-600">{s.body}</p>
              </div>
              {s.to && !s.done ? (
                <Link to={s.to} className="shrink-0 rounded-lg bg-indigo-50 px-3 py-1.5 text-sm font-medium text-indigo-700 hover:bg-indigo-100">
                  {s.to.startsWith('/book/') ? 'Open' : 'Start'}
                </Link>
              ) : (
                !s.done && <span className="shrink-0 rounded-full bg-zinc-100 px-2.5 py-1 text-xs font-medium text-zinc-500">Coming soon</span>
              )}
            </li>
          ))}
        </ol>
      </section>
    </>
  )
}
