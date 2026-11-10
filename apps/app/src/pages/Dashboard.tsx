import { useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { Button, Logo } from '../components/ui'
import { auth, meQuery, tenantsQuery } from '../lib/api'
import { checklist, savedTenant, selectTenant } from '../lib/onboarding'

export function DashboardPage() {
  const { data: me } = useSuspenseQuery(meQuery)
  const { data: tenants } = useSuspenseQuery(tenantsQuery)
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [tenantId, setTenantId] = useState(() => savedTenant())
  const tenant = tenants.find((t) => t.id === tenantId) ?? tenants[0]!
  const steps = checklist(tenant)
  const done = steps.filter((s) => s.done).length

  async function signOut() {
    await auth.logout()
    qc.clear()
    await navigate({ to: '/login' })
  }

  return (
    <div className="min-h-svh bg-zinc-50">
      <header className="border-b border-zinc-200 bg-white">
        <div className="mx-auto flex h-16 max-w-5xl items-center gap-4 px-4">
          <Logo />
          <span className="text-zinc-300" aria-hidden>
            /
          </span>
          {tenants.length > 1 ? (
            <select
              aria-label="Switch space"
              className="rounded-md bg-transparent py-1 text-sm font-medium text-zinc-900 focus:outline-2 focus:outline-indigo-600"
              value={tenant.id}
              onChange={(e) => {
                selectTenant(e.target.value)
                setTenantId(e.target.value)
              }}
            >
              {tenants.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </select>
          ) : (
            <span className="text-sm font-medium text-zinc-900">{tenant.name}</span>
          )}
          <div className="ml-auto flex items-center gap-2">
            <Link to="/onboarding" className="hidden text-sm text-zinc-600 hover:text-zinc-900 sm:inline">
              New space
            </Link>
            <Button variant="ghost" onClick={signOut}>
              Sign out
            </Button>
          </div>
        </div>
      </header>

      <main className="mx-auto max-w-5xl px-4 py-10">
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
                {!s.done && <span className="shrink-0 rounded-full bg-zinc-100 px-2.5 py-1 text-xs font-medium text-zinc-500">Coming soon</span>}
              </li>
            ))}
          </ol>
        </section>
      </main>
    </div>
  )
}
