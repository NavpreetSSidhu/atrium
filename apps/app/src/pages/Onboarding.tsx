import { useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useState, type FormEvent } from 'react'
import { Button, Field, Input, Shell } from '../components/ui'
import { ApiError, createTenant, meQuery, selectTenant, tenantsQuery, setName as saveName, type Focus, type Tenant } from '../lib/api'
import { slugify } from '../lib/format'

const offers: { id: Focus; title: string; body: string }[] = [
  { id: 'courses', title: 'Courses & live classes', body: 'Recorded lessons, cohorts, webinars and events.' },
  { id: 'appointments', title: 'Appointments & consultations', body: 'Doctors, lawyers, coaches and consultants.' },
]

const steps = ['You', 'Your offer', 'Your space'] as const

export function OnboardingPage() {
  const { data: me } = useSuspenseQuery(meQuery)
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [step, setStep] = useState(0)
  const [name, setName] = useState(me.name)
  const [focus, setFocus] = useState<Focus[]>([])
  const [space, setSpace] = useState('')
  const [slug, setSlug] = useState('')
  const [slugEdited, setSlugEdited] = useState(false)
  const [created, setCreated] = useState<Tenant>()
  const [error, setError] = useState<{ field: 'name' | 'space' | 'slug'; text: string }>()
  const [busy, setBusy] = useState(false)

  async function submitName(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      qc.setQueryData(meQuery.queryKey, await saveName(name))
      setError(undefined)
      setStep(1)
    } catch {
      setError({ field: 'name', text: 'Use 1 to 80 characters.' })
    } finally {
      setBusy(false)
    }
  }

  async function submitSpace(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      const t = await createTenant({ name: space, slug, focus })
      // Refetch now: the dashboard's ensureQueryData would otherwise reuse a stale (pre-create) list.
      await qc.fetchQuery({ ...tenantsQuery, staleTime: 0 })
      selectTenant(t.id)
      setError(undefined)
      setCreated(t)
    } catch (err) {
      setError(
        err instanceof ApiError && err.status === 409
          ? { field: 'slug', text: `${slug}.atrium.in is taken. Try another.` }
          : { field: 'slug', text: 'Use 3–40 lowercase letters, numbers or hyphens. Some words are reserved.' },
      )
    } finally {
      setBusy(false)
    }
  }

  if (created) {
    return (
      <Shell wide>
        <div className="text-center">
          <div className="mx-auto grid size-12 place-items-center rounded-full bg-emerald-100 text-2xl text-emerald-700" aria-hidden>
            ✓
          </div>
          <h1 className="mt-4 text-2xl font-semibold text-zinc-900">{created.name} is ready</h1>
          <p className="mt-2 text-zinc-600">
            Your site will live at <strong className="text-zinc-900">{created.slug}.atrium.in</strong>. Next, we'll walk you through setting it up.
          </p>
          <Button className="mt-8 w-full sm:w-auto" onClick={() => navigate({ to: '/' })}>
            Go to your dashboard
          </Button>
        </div>
      </Shell>
    )
  }

  return (
    <Shell wide>
      <ol className="mb-8 flex gap-2" aria-label="Onboarding progress">
        {steps.map((label, i) => (
          <li key={label} className="flex-1" aria-current={i === step ? 'step' : undefined}>
            <div className={`h-1.5 rounded-full ${i <= step ? 'bg-indigo-600' : 'bg-zinc-200'}`} />
            <span className={`mt-2 block text-xs font-medium ${i <= step ? 'text-zinc-900' : 'text-zinc-400'}`}>
              {i + 1}. {label}
            </span>
          </li>
        ))}
      </ol>

      {step === 0 && (
        <form onSubmit={submitName} className="space-y-6">
          <div>
            <h1 className="text-2xl font-semibold text-zinc-900">Welcome to Atrium</h1>
            <p className="mt-2 text-zinc-600">Let's set up your space in under a minute. First, what should we call you?</p>
          </div>
          <Field label="Your name" error={error?.field === 'name' ? error.text : undefined}>
            <Input autoFocus autoComplete="name" placeholder="Dr. Asha Rao" value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Button type="submit" className="w-full" disabled={busy || !name.trim()}>
            Continue
          </Button>
        </form>
      )}

      {step === 1 && (
        <div className="space-y-6">
          <div>
            <h1 className="text-2xl font-semibold text-zinc-900">What will you offer?</h1>
            <p className="mt-2 text-zinc-600">Pick one or both. We'll tailor your setup steps, and you can change this later.</p>
          </div>
          <div className="grid gap-3">
            {offers.map((o) => {
              const on = focus.includes(o.id)
              return (
                <button
                  key={o.id}
                  type="button"
                  aria-pressed={on}
                  onClick={() => setFocus(on ? focus.filter((f) => f !== o.id) : [...focus, o.id])}
                  className={`rounded-xl border p-4 text-left transition-colors focus-visible:outline-2 focus-visible:outline-indigo-600 ${on ? 'border-indigo-600 bg-indigo-50 ring-1 ring-indigo-600' : 'border-zinc-200 hover:border-zinc-300'}`}
                >
                  <span className="flex items-center justify-between font-medium text-zinc-900">
                    {o.title}
                    <span className={`grid size-5 place-items-center rounded-full border text-xs ${on ? 'border-indigo-600 bg-indigo-600 text-white' : 'border-zinc-300'}`} aria-hidden>
                      {on && '✓'}
                    </span>
                  </span>
                  <span className="mt-1 block text-sm text-zinc-600">{o.body}</span>
                </button>
              )
            })}
          </div>
          <div className="flex gap-3">
            <Button variant="ghost" onClick={() => setStep(0)}>
              Back
            </Button>
            <Button className="flex-1" disabled={!focus.length} onClick={() => setStep(2)}>
              Continue
            </Button>
          </div>
        </div>
      )}

      {step === 2 && (
        <form onSubmit={submitSpace} className="space-y-6">
          <div>
            <h1 className="text-2xl font-semibold text-zinc-900">Name your space</h1>
            <p className="mt-2 text-zinc-600">This is your brand: your clinic, practice, academy or studio.</p>
          </div>
          <Field label="Space name">
            <Input
              autoFocus
              placeholder="Rao Skin Clinic"
              value={space}
              onChange={(e) => {
                setSpace(e.target.value)
                if (!slugEdited) setSlug(slugify(e.target.value))
              }}
            />
          </Field>
          <Field label="Web address" error={error?.field === 'slug' ? error.text : undefined} hint="You can connect your own domain later.">
            <div className="flex items-center rounded-lg border border-zinc-300 bg-white focus-within:border-indigo-600 focus-within:ring-2 focus-within:ring-indigo-600/20">
              <input
                className="h-11 min-w-0 flex-1 rounded-l-lg bg-transparent px-3 text-zinc-900 focus:outline-none"
                value={slug}
                placeholder="rao-skin-clinic"
                onChange={(e) => {
                  setSlugEdited(true)
                  setSlug(slugify(e.target.value))
                }}
              />
              <span className="pr-3 text-zinc-500">.atrium.in</span>
            </div>
          </Field>
          <div className="flex gap-3">
            <Button variant="ghost" type="button" onClick={() => setStep(1)}>
              Back
            </Button>
            <Button type="submit" className="flex-1" disabled={busy || !space.trim() || slug.length < 3}>
              {busy ? 'Creating…' : 'Create my space'}
            </Button>
          </div>
        </form>
      )}
    </Shell>
  )
}
