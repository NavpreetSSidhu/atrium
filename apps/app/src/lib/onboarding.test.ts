import assert from 'node:assert/strict'
import { test } from 'node:test'
import { checklist } from './onboarding.ts'

const space = { id: '1', slug: 'rao', name: 'Rao', role: 'owner' }
const none = { services: 0, hasHours: false }

test('checklist is tailored to what the space offers', () => {
  const titles = (focus: ('courses' | 'appointments')[]) => checklist({ ...space, focus }, none).map((s) => s.title)
  assert.ok(titles(['appointments']).includes('Set your availability'))
  assert.ok(!titles(['appointments']).includes('Create your first course'))
  assert.ok(titles(['courses']).includes('Create your first course'))
  assert.equal(titles(['courses', 'appointments']).length, 8)
})

test('appointment steps tick off with real progress', () => {
  const done = (p: typeof none) => checklist({ ...space, focus: ['appointments'] }, p).filter((s) => s.done).map((s) => s.title)
  assert.deepEqual(done(none), ['Create your space'])
  assert.deepEqual(done({ services: 2, hasHours: true }), ['Create your space', 'Add a service', 'Set your availability'])
  const share = checklist({ ...space, focus: ['appointments'] }, none).find((s) => s.title === 'Share your booking page')
  assert.equal(share?.to, '/book/rao')
})
