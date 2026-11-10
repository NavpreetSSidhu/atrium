import { useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { Button, Field, Input } from '../components/ui'
import { availabilityQuery, becomeProvider, providersQuery, saveAvailability, type Availability, type Hours, type Provider } from '../lib/api'
import { fromHHMM, localDate, toHHMM } from '../lib/format'

const days = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']
const order = [1, 2, 3, 4, 5, 6, 0] // show the week starting Monday

export function AvailabilityPage() {
  const { data: providers } = useSuspenseQuery(providersQuery())
  const me = providers.find((p) => p.me)
  return me ? <WeekEditor provider={me} /> : <BecomeProvider />
}

function BecomeProvider() {
  const qc = useQueryClient()
  const [title, setTitle] = useState('')
  const [tz, setTz] = useState(() => Intl.DateTimeFormat().resolvedOptions().timeZone)
  const [busy, setBusy] = useState(false)
  async function submit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      await becomeProvider(title, tz)
      await qc.invalidateQueries()
    } finally {
      setBusy(false)
    }
  }
  return (
    <form onSubmit={submit} className="mx-auto max-w-lg rounded-2xl border border-zinc-200 bg-white p-6">
      <h1 className="text-xl font-semibold text-zinc-900">Do you see clients yourself?</h1>
      <p className="mt-2 text-zinc-600">Set yourself up as a provider so clients can book time on your calendar.</p>
      <div className="mt-6 space-y-4">
        <Field label="How clients see you" hint="For example: Dr. Meera Rao, Dermatologist">
          <Input required autoFocus value={title} onChange={(e) => setTitle(e.target.value)} />
        </Field>
        <Field label="Your timezone">
          <select
            className="h-11 w-full rounded-lg border border-zinc-300 bg-white px-3 text-zinc-900 focus:border-indigo-600 focus:outline-none focus:ring-2 focus:ring-indigo-600/20"
            value={tz}
            onChange={(e) => setTz(e.target.value)}
          >
            {Intl.supportedValuesOf('timeZone').map((z) => (
              <option key={z}>{z}</option>
            ))}
          </select>
        </Field>
      </div>
      <Button type="submit" className="mt-6 w-full" disabled={busy || !title.trim()}>
        Continue
      </Button>
    </form>
  )
}

function WeekEditor({ provider }: { provider: Provider }) {
  const qc = useQueryClient()
  const { data } = useSuspenseQuery(availabilityQuery(provider.id))
  const [week, setWeek] = useState<Hours[][]>(() => days.map((_, d) => data.rules.filter((r) => r.weekday === d)))
  const [overrides, setOverrides] = useState(data.overrides)
  const [dayOff, setDayOff] = useState('')
  const [status, setStatus] = useState<'idle' | 'saving' | 'saved' | 'error'>('idle')

  const setDay = (d: number, hours: Hours[]) => {
    setWeek(week.map((h, i) => (i === d ? hours : h)))
    setStatus('idle')
  }
  const [today] = useState(() => localDate(new Date()))

  async function save() {
    setStatus('saving')
    const doc: Availability = { rules: week.flatMap((hours, weekday) => hours.map((h) => ({ weekday, ...h }))), overrides }
    try {
      await saveAvailability(provider.id, doc)
      await qc.invalidateQueries()
      setStatus('saved')
    } catch {
      setStatus('error')
    }
  }

  return (
    <>
      <div className="mb-6 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold text-zinc-900">Availability</h1>
          <p className="mt-1 text-zinc-600">
            When {provider.title || 'you'} can be booked. Times are in {provider.timezone}.
          </p>
        </div>
        <div className="flex items-center gap-3">
          <span role="status" className="text-sm text-zinc-500">
            {status === 'saved' ? 'Saved' : status === 'error' ? <span className="text-red-600">Each range must end after it starts.</span> : ''}
          </span>
          <Button onClick={save} disabled={status === 'saving'}>
            {status === 'saving' ? 'Saving…' : 'Save changes'}
          </Button>
        </div>
      </div>

      <section className="rounded-2xl border border-zinc-200 bg-white" aria-labelledby="weekly">
        <h2 id="weekly" className="border-b border-zinc-200 p-5 font-semibold text-zinc-900">
          Weekly hours
        </h2>
        <ul className="divide-y divide-zinc-100">
          {order.map((d) => {
            const hours = week[d]!
            return (
              <li key={d} className="flex flex-wrap items-start gap-4 p-4 sm:flex-nowrap">
                <label className="flex w-36 shrink-0 items-center gap-3 pt-2.5 font-medium text-zinc-900">
                  <input
                    type="checkbox"
                    className="size-4 accent-indigo-600"
                    checked={hours.length > 0}
                    onChange={(e) => setDay(d, e.target.checked ? [{ start_min: 9 * 60, end_min: 17 * 60 }] : [])}
                  />
                  {days[d]}
                </label>
                <div className="flex-1 space-y-2">
                  {hours.length === 0 && <p className="pt-2.5 text-sm text-zinc-400">Unavailable</p>}
                  {hours.map((h, i) => (
                    <div key={i} className="flex items-center gap-2">
                      <TimeInput label={`${days[d]} start`} value={h.start_min} onChange={(m) => setDay(d, hours.with(i, { ...h, start_min: m }))} />
                      <span className="text-zinc-400">–</span>
                      <TimeInput label={`${days[d]} end`} value={h.end_min} onChange={(m) => setDay(d, hours.with(i, { ...h, end_min: m }))} />
                      <button
                        type="button"
                        aria-label={`Remove ${days[d]} range`}
                        className="grid size-9 place-items-center rounded-lg text-zinc-400 hover:bg-zinc-100 hover:text-zinc-700"
                        onClick={() => setDay(d, hours.filter((_, j) => j !== i))}
                      >
                        ×
                      </button>
                    </div>
                  ))}
                </div>
                {hours.length > 0 && (
                  <Button
                    variant="ghost"
                    className="h-9"
                    onClick={() => {
                      const last = hours.at(-1)!
                      setDay(d, [...hours, { start_min: Math.min(last.end_min + 60, 1380), end_min: Math.min(last.end_min + 120, 1440) }])
                    }}
                  >
                    + Add hours
                  </Button>
                )}
              </li>
            )
          })}
        </ul>
      </section>

      <section className="mt-6 rounded-2xl border border-zinc-200 bg-white p-5" aria-labelledby="time-off">
        <h2 id="time-off" className="font-semibold text-zinc-900">
          Days off
        </h2>
        <p className="mt-1 text-sm text-zinc-600">Holidays, leave or conferences. Nobody can book you on these dates.</p>
        <div className="mt-4 flex flex-wrap items-end gap-3">
          <div className="w-48">
            <Field label="Date">
              <Input type="date" min={today} value={dayOff} onChange={(e) => setDayOff(e.target.value)} />
            </Field>
          </div>
          <Button
            variant="ghost"
            disabled={!dayOff || overrides.some((o) => o.date === dayOff)}
            onClick={() => {
              setOverrides([...overrides, { date: dayOff, start_min: null, end_min: null }].sort((a, b) => a.date.localeCompare(b.date)))
              setDayOff('')
              setStatus('idle')
            }}
          >
            + Add day off
          </Button>
        </div>
        <ul className="mt-4 flex flex-wrap gap-2">
          {overrides
            .filter((o) => o.date >= today)
            .map((o) => (
              <li key={o.date + o.start_min} className="flex items-center gap-1 rounded-full bg-zinc-100 py-1 pr-1 pl-3 text-sm text-zinc-700">
                {new Date(o.date + 'T00:00').toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })}
                {o.start_min !== null && ` (${toHHMM(o.start_min)}–${toHHMM(o.end_min!)})`}
                <button
                  type="button"
                  aria-label={`Remove ${o.date}`}
                  className="grid size-6 place-items-center rounded-full hover:bg-zinc-200"
                  onClick={() => {
                    setOverrides(overrides.filter((x) => x !== o))
                    setStatus('idle')
                  }}
                >
                  ×
                </button>
              </li>
            ))}
        </ul>
      </section>
    </>
  )
}

function TimeInput({ label, value, onChange }: { label: string; value: number; onChange: (min: number) => void }) {
  return (
    <input
      type="time"
      aria-label={label}
      step={300}
      className="h-10 rounded-lg border border-zinc-300 bg-white px-2 text-zinc-900 tabular-nums focus:border-indigo-600 focus:outline-none focus:ring-2 focus:ring-indigo-600/20"
      value={toHHMM(value)}
      onChange={(e) => e.target.value && onChange(fromHHMM(e.target.value))}
    />
  )
}
