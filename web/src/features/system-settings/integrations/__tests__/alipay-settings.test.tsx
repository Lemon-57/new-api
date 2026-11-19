import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import {
  BILLING_SECTION_IDS,
  getBillingSectionContent,
  type BillingSectionId,
} from '../../billing/section-registry'
import type { BillingSettings } from '../../types'

afterEach(() => vi.restoreAllMocks())

it('shows Alipay settings and changes a URL without replacing the private key', async () => {
  const section = (BILLING_SECTION_IDS as readonly string[]).find(
    (entry) => entry === 'alipay'
  )
  expect(section).toBe('alipay')
  const key = {
    app_id: 'synthetic-merchant',
    public_key: 'fixture-public-key',
    private_key_configured: true,
    notify_url: 'https://merchant.example/api/user/alipay/notify',
    return_url: 'https://merchant.example/wallet',
    revision: 'revision-one',
  }
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, data: key },
  })
  const save = vi.spyOn(api, 'put').mockResolvedValue({
    data: {
      success: true,
      data: {
        ...key,
        return_url: 'https://merchant.example/history',
        revision: 'revision-two',
      },
    },
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      {getBillingSectionContent(
        section as BillingSectionId,
        {} as BillingSettings
      )}
    </QueryClientProvider>
  )
  expect(await screen.findByLabelText('Alipay App ID')).toHaveValue(key.app_id)
  expect(screen.getByLabelText('API Private Key')).toHaveValue('')
  const user = userEvent.setup()
  const returnUrl = screen.getByLabelText('Alipay return URL')
  await user.clear(returnUrl)
  await user.type(returnUrl, 'https://merchant.example/history')
  await user.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() =>
    expect(save).toHaveBeenCalledWith('/api/option/alipay', {
      app_id: key.app_id,
      public_key: key.public_key,
      notify_url: key.notify_url,
      return_url: 'https://merchant.example/history',
      private_key: '',
      revision: key.revision,
    })
  )
  expect(screen.getByLabelText('API Private Key')).toHaveValue('')
})
