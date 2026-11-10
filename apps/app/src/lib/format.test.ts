import assert from 'node:assert/strict'
import { test } from 'node:test'
import { slugify, toE164 } from './format.ts'

test('toE164', () => {
  assert.equal(toE164('98765 43210'), '+919876543210')
  assert.equal(toE164('+91 98765-43210'), '+919876543210')
  assert.equal(toE164('(+1) 415-555-0100'), '+14155550100')
  assert.equal(toE164('447911123456'), '+447911123456')
})

test('slugify matches the API slug rule', () => {
  const rule = /^[a-z0-9](?:[a-z0-9-]{1,38}[a-z0-9])$/ // apps/api/internal/tenancy
  assert.equal(slugify("Dr. Rao's Skin Clinic"), 'dr-raos-skin-clinic')
  assert.equal(slugify('  Café — Studio  '), 'cafe-studio')
  assert.equal(slugify('a'.repeat(39) + ' b'), 'a'.repeat(39))
  for (const s of ["Dr. Rao's Skin Clinic", 'Café — Studio', 'x'.repeat(60), 'Yoga  With   Meera!']) {
    assert.match(slugify(s), rule, s)
  }
})
