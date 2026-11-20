import { cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'

import { ModerationAuditDetails } from '../components/moderation-audit-details'
import type { ModerationAuditRow } from '../moderation-audit-api'

const record: ModerationAuditRow = {
  id: 1,
  request_id: 'fixture-audit-request',
  user_id: 15,
  instance_id: 'rp_moderation_fixture',
  instance_name: 'Fixture',
  model: 'synthetic-model',
  engine: 'typesafe',
  engine_model: 'jev-fixture',
  stage: 'ingress',
  mode: 'enforce',
  decision: 'blocked',
  category: 'violence',
  auto_disabled: false,
  created_at: 1795180000,
  expires_at: 1795266400,
}

afterEach(cleanup)

test('does not imply that unsaved input exists', () => {
  render(<ModerationAuditDetails item={record} onClose={vi.fn()} />)
  const dialog = screen.getByRole('dialog', { name: 'Content audit details' })
  expect(within(dialog).getByText('No excerpt was stored.')).toBeVisible()
  expect(within(dialog).queryByRole('table')).not.toBeInTheDocument()
})

test('shows the stored excerpt as text and separates scores from thresholds', () => {
  const item = {
    ...record,
    scores: '{"violence":0.9432}',
    thresholds: '{"violence":0.8}',
    input_excerpt: '<b>fixture [redacted]</b>',
  }
  render(<ModerationAuditDetails item={item} onClose={vi.fn()} />)
  const dialog = screen.getByRole('dialog', { name: 'Content audit details' })
  expect(within(dialog).getByText(item.input_excerpt)).toBeVisible()
  const table = within(dialog).getByRole('table', {
    name: 'Category scores and thresholds',
  })
  expect(
    within(table).getByRole('columnheader', { name: 'Score' })
  ).toBeVisible()
  expect(within(table).getByRole('cell', { name: '0.9432' })).toBeVisible()
  expect(within(table).getByRole('cell', { name: '0.8' })).toBeVisible()
})
