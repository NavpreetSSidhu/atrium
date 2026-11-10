import { useNavigate } from '@tanstack/react-router'
import { useState, type FormEvent } from 'react'
import { Button, Field, Input, Shell } from '../components/ui'
import { ApiError, auth } from '../lib/api'
import { toE164 } from '../lib/format'

type Channel = 'sms' | 'email'

function errorText(e: unknown, channel: Channel) {
  if (!(e instanceof ApiError)) return 'Something went wrong. Check your connection and try again.'
  if (e.status === 429) return 'Too many codes requested. Try again in an hour.'
  if (e.status === 401) return 'That code is wrong or has expired.'
  if (e.status === 400) return channel === 'sms' ? 'Enter a valid mobile number.' : 'Enter a valid email address.'
  return 'Something went wrong. Please try again.'
}

export function LoginPage() {
  const navigate = useNavigate()
  const [channel, setChannel] = useState<Channel>('sms')
  const [dest, setDest] = useState('')
  const [sentTo, setSentTo] = useState<string | null>(null)
  const [code, setCode] = useState('')
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)

  async function run(fn: () => Promise<void>) {
    setBusy(true)
    setError(undefined)
    try {
      await fn()
    } catch (e) {
      setError(errorText(e, channel))
    } finally {
      setBusy(false)
    }
  }

  const start = (e?: FormEvent) => {
    e?.preventDefault()
    const to = channel === 'sms' ? toE164(dest) : dest.trim().toLowerCase()
    run(async () => {
      await auth.start(channel, to)
      setSentTo(to)
      setCode('')
    })
  }
  const verify = (e: FormEvent) => {
    e.preventDefault()
    run(async () => {
      await auth.verify('sms', sentTo!, code)
      await navigate({ to: '/' })
    })
  }
  const reset = () => {
    setSentTo(null)
    setError(undefined)
  }

  if (sentTo && channel === 'email') {
    return (
      <Shell>
        <h1 className="text-xl font-semibold text-zinc-900">Check your inbox</h1>
        <p className="mt-2 text-sm text-zinc-600">
          We sent a sign-in link to <strong className="text-zinc-900">{sentTo}</strong>. It expires in 10 minutes.
        </p>
        <Button variant="ghost" className="mt-6 w-full" onClick={reset}>
          Use a different email
        </Button>
        <DevHint />
      </Shell>
    )
  }

  if (sentTo) {
    return (
      <Shell>
        <h1 className="text-xl font-semibold text-zinc-900">Enter your code</h1>
        <p className="mt-2 text-sm text-zinc-600">
          We sent a 6-digit code to <strong className="text-zinc-900">{sentTo}</strong>.
        </p>
        <form onSubmit={verify} className="mt-6 space-y-4">
          <Field label="Code" error={error}>
            <Input
              autoFocus
              inputMode="numeric"
              autoComplete="one-time-code"
              pattern="[0-9]{6}"
              maxLength={6}
              value={code}
              onChange={(e) => setCode(e.target.value.replace(/\D/g, ''))}
              className="h-12 w-full rounded-lg border border-zinc-300 text-center font-mono text-2xl tracking-[0.5em] focus:border-indigo-600 focus:outline-none focus:ring-2 focus:ring-indigo-600/20"
            />
          </Field>
          <Button type="submit" className="w-full" disabled={busy || code.length !== 6}>
            {busy ? 'Checking…' : 'Continue'}
          </Button>
        </form>
        <div className="mt-3 flex justify-between">
          <Button variant="ghost" onClick={reset}>
            Change number
          </Button>
          <Button variant="ghost" onClick={() => start()} disabled={busy}>
            Resend code
          </Button>
        </div>
        <DevHint />
      </Shell>
    )
  }

  return (
    <Shell>
      <h1 className="text-xl font-semibold text-zinc-900">Sign in or create an account</h1>
      <p className="mt-2 text-sm text-zinc-600">No password needed. We'll send you a one-time code.</p>
      <div role="tablist" className="mt-6 grid grid-cols-2 rounded-lg bg-zinc-100 p-1 text-sm font-medium">
        {(['sms', 'email'] as const).map((c) => (
          <button
            key={c}
            role="tab"
            type="button"
            aria-selected={channel === c}
            onClick={() => {
              setChannel(c)
              setDest('')
              setError(undefined)
            }}
            className={`h-9 rounded-md transition-colors ${channel === c ? 'bg-white text-zinc-900 shadow-sm' : 'text-zinc-500 hover:text-zinc-800'}`}
          >
            {c === 'sms' ? 'Mobile' : 'Email'}
          </button>
        ))}
      </div>
      <form onSubmit={start} className="mt-4 space-y-4">
        {channel === 'sms' ? (
          <Field label="Mobile number" hint="Indian numbers can skip the +91." error={error}>
            <Input autoFocus type="tel" autoComplete="tel" placeholder="98765 43210" value={dest} onChange={(e) => setDest(e.target.value)} />
          </Field>
        ) : (
          <Field label="Email" error={error}>
            <Input autoFocus type="email" autoComplete="email" placeholder="you@example.com" value={dest} onChange={(e) => setDest(e.target.value)} />
          </Field>
        )}
        <Button type="submit" className="w-full" disabled={busy || !dest.trim()}>
          {busy ? 'Sending…' : channel === 'sms' ? 'Send code' : 'Email me a link'}
        </Button>
      </form>
    </Shell>
  )
}

function DevHint() {
  if (!import.meta.env.DEV) return null
  return <p className="mt-6 rounded-lg bg-amber-50 p-3 text-xs text-amber-800">Dev mode: codes and links are printed in the API log, not sent.</p>
}
