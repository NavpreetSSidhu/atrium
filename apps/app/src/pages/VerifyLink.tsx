import { getRouteApi, Link } from '@tanstack/react-router'
import { Shell } from '../components/ui'

const route = getRouteApi('/auth/verify')

// Shown only when the magic link fails; on success the route loader redirects to the dashboard.
export function VerifyLinkPage() {
  const { error } = route.useLoaderData()
  return (
    <Shell>
      <h1 className="text-xl font-semibold text-zinc-900">This link didn't work</h1>
      <p className="mt-2 text-sm text-zinc-600">{error}</p>
      <Link to="/login" className="mt-6 inline-block text-sm font-medium text-indigo-600 hover:text-indigo-500">
        Get a new link →
      </Link>
    </Shell>
  )
}
