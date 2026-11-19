import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { expect, it } from 'vitest'

import type { PlanRecord } from '../../types'
import { SubscriptionPurchaseDialog } from './subscription-purchase-dialog'

it('offers Epay only when the selected plan advertises an Epay method', () => {
  const plan: PlanRecord = {
    plan: {
      id: 1,
      title: 'Synthetic plan',
      price_amount: 10,
      currency: 'USD',
      duration_unit: 'month',
      duration_value: 1,
      quota_reset_period: 'monthly',
      enabled: true,
      sort_order: 0,
      allow_balance_pay: true,
      allow_wallet_overflow: false,
      max_purchase_per_user: 0,
      total_amount: 500000,
    },
    payment: { stripe: false, creem: false, waffo_pancake: false, epay: [] },
  }
  const client = new QueryClient()
  const dialog = (record: PlanRecord) => (
    <QueryClientProvider client={client}>
      <SubscriptionPurchaseDialog open plan={record} onOpenChange={() => {}} />
    </QueryClientProvider>
  )
  const { rerender } = render(dialog(plan))
  expect(screen.queryByRole('button', { name: 'Pay' })).not.toBeInTheDocument()
  expect(
    screen.getByRole('button', { name: 'Pay with Balance' })
  ).toBeInTheDocument()

  const configured: PlanRecord = {
    ...plan,
    payment: {
      stripe: false,
      creem: false,
      waffo_pancake: false,
      epay: [{ type: 'alipay', name: 'Alipay via Epay' }],
    },
  }
  rerender(dialog(configured))
  expect(screen.getByRole('button', { name: 'Pay' })).toBeEnabled()
  expect(screen.getByRole('combobox')).toHaveValue('Alipay via Epay')
})
