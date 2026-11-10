import { QueryClient } from '@tanstack/react-query'
import { createRootRouteWithContext, createRoute, createRouter, Outlet, redirect } from '@tanstack/react-router'
import { AppShell } from './components/AppShell'
import { ApiError, auth, currentTenant, joinTenant, meQuery, providersQuery, selectTenant, servicesQuery, tenantsQuery } from './lib/api'
import { safeRedirect } from './lib/format'
import { AppointmentsPage } from './pages/Appointments'
import { AvailabilityPage } from './pages/Availability'
import { BookPage } from './pages/Book'
import { AFTER_LOGIN, LoginPage } from './pages/Login'
import { OnboardingPage } from './pages/Onboarding'
import { OverviewPage } from './pages/Overview'
import { ServicesPage } from './pages/Services'
import { VerifyLinkPage } from './pages/VerifyLink'

export const queryClient = new QueryClient({ defaultOptions: { queries: { staleTime: 30_000 } } })

const root = createRootRouteWithContext<{ queryClient: QueryClient }>()({ component: Outlet })

const login = createRoute({
  getParentRoute: () => root,
  path: '/login',
  validateSearch: (s: Record<string, unknown>): { redirect?: string } => (typeof s.redirect === 'string' ? { redirect: s.redirect } : {}),
  beforeLoad: async ({ search }) => {
    if (await auth.signedIn()) throw redirect({ href: safeRedirect(search.redirect) })
  },
  component: LoginPage,
})

// Magic link target: verifies in the loader (runs once, unlike a StrictMode effect) then redirects.
const verifyLink = createRoute({
  getParentRoute: () => root,
  path: '/auth/verify',
  validateSearch: (s: Record<string, unknown>) => ({ email: String(s.email ?? ''), code: String(s.code ?? '') }),
  loaderDeps: ({ search }) => search,
  loader: async ({ deps }) => {
    try {
      await auth.verify('email', deps.email, deps.code)
    } catch (e) {
      return { error: e instanceof ApiError && e.status === 401 ? 'It has expired or was already used.' : 'Something went wrong. Please try again.' }
    }
    let to = '/'
    try {
      to = safeRedirect(localStorage.getItem(AFTER_LOGIN))
      localStorage.removeItem(AFTER_LOGIN)
    } catch {}
    throw redirect({ href: to })
  },
  component: VerifyLinkPage,
})

const signedIn = createRoute({
  getParentRoute: () => root,
  id: 'signed-in',
  beforeLoad: async ({ location }) => {
    if (!(await auth.signedIn())) throw redirect({ to: '/login', search: { redirect: location.href } })
  },
})

const onboarding = createRoute({
  getParentRoute: () => signedIn,
  path: '/onboarding',
  loader: ({ context: { queryClient } }) => queryClient.ensureQueryData(meQuery),
  component: OnboardingPage,
})

// Public booking page of a space: signing in here joins it as a member.
const bookPage = createRoute({
  getParentRoute: () => signedIn,
  path: '/book/$slug',
  loader: async ({ params, context: { queryClient } }) => {
    const tenant = await joinTenant(params.slug).catch((e) => {
      if (e instanceof ApiError && e.status === 404) throw redirect({ to: '/' })
      throw e
    })
    selectTenant(tenant.id)
    await queryClient.invalidateQueries({ queryKey: tenantsQuery.queryKey })
    await Promise.all([queryClient.ensureQueryData(servicesQuery()), queryClient.ensureQueryData(providersQuery()), queryClient.ensureQueryData(meQuery)])
    return tenant
  },
  component: BookPage,
})

// Everything inside a space. Picks a valid space and sends people without one to onboarding.
const space = createRoute({
  getParentRoute: () => signedIn,
  id: 'space',
  loader: async ({ context: { queryClient } }) => {
    const [tenants] = await Promise.all([queryClient.ensureQueryData(tenantsQuery), queryClient.ensureQueryData(meQuery)])
    if (!tenants.length) throw redirect({ to: '/onboarding' })
    if (!tenants.some((t) => t.id === currentTenant())) selectTenant(tenants[0]!.id)
  },
  component: AppShell,
})

const overview = createRoute({ getParentRoute: () => space, path: '/', component: OverviewPage })
const appointments = createRoute({ getParentRoute: () => space, path: '/appointments', component: AppointmentsPage })
const services = createRoute({ getParentRoute: () => space, path: '/services', component: ServicesPage })
const availability = createRoute({ getParentRoute: () => space, path: '/availability', component: AvailabilityPage })

export const router = createRouter({
  routeTree: root.addChildren([
    login,
    verifyLink,
    signedIn.addChildren([
      onboarding,
      bookPage,
      space.addChildren([overview, appointments, services, availability]),
    ]),
  ]),
  context: { queryClient },
  defaultPreloadStaleTime: 0,
  defaultPendingComponent: () => <p className="p-8 text-zinc-500">Loading…</p>,
})

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
