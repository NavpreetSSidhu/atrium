import assert from 'node:assert/strict'
import { test } from 'node:test'
import { checklist } from './onboarding.ts'

const space = { id: '1', slug: 'rao', name: 'Rao', role: 'owner' }

test('checklist is tailored to what the space offers', () => {
  const titles = (focus: ('courses' | 'appointments')[]) => checklist({ ...space, focus }).map((s) => s.title)
  assert.ok(titles(['appointments']).includes('Set your availability'))
  assert.ok(!titles(['appointments']).includes('Create your first course'))
  assert.ok(titles(['courses']).includes('Create your first course'))
  assert.equal(titles(['courses', 'appointments']).length, 7)
  assert.equal(checklist({ ...space, focus: [] }).filter((s) => s.done).length, 1)
})
