import { useSuspenseQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { BookingList } from '../components/Bookings'
import { useTenant } from '../lib/space'
import { bookingsQuery } from '../lib/api'
import { upcoming } from '../lib/format'

export function AppointmentsPage() {
  const { tenant } = useTenant()
  const { data: bookings } = useSuspenseQuery(bookingsQuery(...upcoming()))
  return (
    <>
      <h1 className="text-2xl font-semibold text-zinc-900">Appointments</h1>
      <p className="mt-1 mb-6 text-zinc-600">Upcoming in the next 60 days.</p>
      <BookingList
        bookings={bookings}
        perspective="business"
        empty={
          <>
            No appointments yet. Share{' '}
            <Link to="/book/$slug" params={{ slug: tenant.slug }} className="font-medium text-indigo-600">
              your booking page
            </Link>{' '}
            to get your first one.
          </>
        }
      />
    </>
  )
}
