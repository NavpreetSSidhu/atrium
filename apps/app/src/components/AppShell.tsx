import { useQueryClient } from '@tanstack/react-query'
import { Link, Outlet, useNavigate, useRouter } from '@tanstack/react-router'
import { auth, selectTenant } from '../lib/api'
import { isStaff, useTenant } from '../lib/space'
import { Button, Logo } from './ui'

export function AppShell() {
  const { tenants, tenant } = useTenant()
  const qc = useQueryClient()
  const router = useRouter()
  const navigate = useNavigate()
  const nav = [
    { to: '/', label: isStaff(tenant) || tenant.role === 'provider' ? 'Overview' : 'My appointments', show: true },
    { to: '/appointments', label: 'Appointments', show: isStaff(tenant) || tenant.role === 'provider' },
    { to: '/services', label: 'Services', show: isStaff(tenant) },
    { to: '/availability', label: 'Availability', show: isStaff(tenant) || tenant.role === 'provider' },
  ].filter((n) => n.show)

  async function signOut() {
    await auth.logout()
    qc.clear()
    await navigate({ to: '/login', search: {} })
  }

  return (
    <div className="min-h-svh bg-zinc-50">
      <header className="border-b border-zinc-200 bg-white">
        <div className="mx-auto flex h-16 max-w-5xl items-center gap-3 px-4">
          <Logo />
          <span className="text-zinc-300" aria-hidden>
            /
          </span>
          {tenants.length > 1 ? (
            <select
              aria-label="Switch space"
              className="min-w-0 truncate rounded-md bg-transparent py-1 text-sm font-medium text-zinc-900 focus:outline-2 focus:outline-indigo-600"
              value={tenant.id}
              onChange={(e) => {
                selectTenant(e.target.value)
                router.invalidate()
                navigate({ to: '/' })
              }}
            >
              {tenants.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </select>
          ) : (
            <span className="truncate text-sm font-medium text-zinc-900">{tenant.name}</span>
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
        {nav.length > 1 && (
          <nav className="mx-auto flex max-w-5xl gap-1 overflow-x-auto px-4" aria-label="Sections">
            {nav.map((n) => (
              <Link
                key={n.to}
                to={n.to}
                activeOptions={{ exact: true }}
                className="border-b-2 border-transparent px-3 py-2.5 text-sm font-medium whitespace-nowrap text-zinc-500 hover:text-zinc-900"
                activeProps={{ className: '!border-indigo-600 !text-zinc-900', 'aria-current': 'page' }}
              >
                {n.label}
              </Link>
            ))}
          </nav>
        )}
      </header>
      <main className="mx-auto max-w-5xl px-4 py-8">
        <Outlet />
      </main>
    </div>
  )
}
