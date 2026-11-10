import { useSuspenseQuery } from '@tanstack/react-query'
import { currentTenant, tenantsQuery, type Tenant } from './api'

export const isStaff = (t: Tenant) => ['owner', 'admin', 'staff'].includes(t.role)

// The tenant the shell is showing. The space layout loader guarantees one is selected.
export function useTenant() {
  const { data: tenants } = useSuspenseQuery(tenantsQuery)
  return { tenants, tenant: tenants.find((t) => t.id === currentTenant()) ?? tenants[0]! }
}
