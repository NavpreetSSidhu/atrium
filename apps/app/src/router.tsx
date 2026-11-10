import { QueryClient } from '@tanstack/react-query'
import { createRootRouteWithContext, createRoute, createRouter, Outlet, redirect } from '@tanstack/react-router'
import { ApiError, auth, meQuery, tenantsQuery } from './lib/api'
import { DashboardPage } from './pages/Dashboard'
import { LoginPage } from './pages/Login'
import { OnboardingPage } from './pages/Onboarding'
import { VerifyLinkPage } from './pages/VerifyLink'

export const queryClient = new QueryClient({ defaultOptions: { queries: { staleTime: 30_000 } } })

const root = createRootRouteWithContext<{ queryClient: QueryClient }>()({ component: Outlet })

const login = createRoute({
  getParentRoute: () => root,
  path: '/login',
  beforeLoad: async () => {
    if (await auth.signedIn()) throw redirect({ to: '/' })
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
    throw redirect({ to: '/' })
  },
  component: VerifyLinkPage,
})

const signedIn = createRoute({
  getParentRoute: () => root,
  id: 'signed-in',
  beforeLoad: async () => {
    if (!(await auth.signedIn())) throw redirect({ to: '/login' })
  },
})

const dashboard = createRoute({
  getParentRoute: () => signedIn,
  path: '/',
  loader: async ({ context: { queryClient } }) => {
    const [tenants] = await Promise.all([queryClient.ensureQueryData(tenantsQuery), queryClient.ensureQueryData(meQuery)])
    if (!tenants.length) throw redirect({ to: '/onboarding' })
  },
  component: DashboardPage,
})

const onboarding = createRoute({
  getParentRoute: () => signedIn,
  path: '/onboarding',
  loader: ({ context: { queryClient } }) => queryClient.ensureQueryData(meQuery),
  component: OnboardingPage,
})

export const router = createRouter({
  routeTree: root.addChildren([login, verifyLink, signedIn.addChildren([dashboard, onboarding])]),
  context: { queryClient },
  defaultPreloadStaleTime: 0,
})

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
