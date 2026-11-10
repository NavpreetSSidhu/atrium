import assert from 'node:assert/strict'
import { test } from 'node:test'
import { fromHHMM, rupees, safeRedirect, slugify, toE164, toHHMM } from './format.ts'

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

test('rupees', () => {
  assert.equal(rupees(0), 'Free')
  assert.equal(rupees(50000), '₹500')
  assert.equal(rupees(149950), '₹1,499.50')
})

test('HH:MM round trip', () => {
  assert.equal(toHHMM(540), '09:00')
  assert.equal(toHHMM(1440), '23:59')
  assert.equal(fromHHMM('17:30'), 1050)
  for (const m of [0, 61, 1439]) assert.equal(fromHHMM(toHHMM(m)), m)
})

test('safeRedirect blocks other sites', () => {
  assert.equal(safeRedirect('/book/rao'), '/book/rao')
  for (const bad of ['//evil.com', '/\\evil.com', 'https://evil.com', undefined, 42]) assert.equal(safeRedirect(bad), '/')
})
