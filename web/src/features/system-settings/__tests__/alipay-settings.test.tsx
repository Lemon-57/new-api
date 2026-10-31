/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../components/settings-page-context'
import { PaymentSettingsSection } from '../integrations/payment-settings-section'

afterEach(() => vi.restoreAllMocks())

function renderSettings() {
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  })
  const actions = document.createElement('div')
  document.body.append(actions)
  const view = render(
    <QueryClientProvider client={queryClient}>
      <SettingsPageProvider actionsContainer={actions}>
        <PaymentSettingsSection
          defaultValues={{
            PayAddress: '',
            EpayId: '',
            EpayKey: '',
            Price: 7.2,
            MinTopUp: 1,
            CustomCallbackAddress: '',
            PayMethods: '[]',
            AmountOptions: '[1]',
            AmountDiscount: '{}',
            AlipayEnabled: false,
            AlipaySandbox: true,
            AlipayAppId: 'stored-app',
            AlipayPrivateKey: '',
            AlipayPublicKey: '',
            AlipaySellerId: 'stored-seller',
            AlipayNotifyUrl: 'http://127.0.0.1:47850/api/user/alipay/notify',
            AlipayReturnUrl: '',
            AlipayCNYPerUSD: '7.2',
            StripeApiSecret: '',
            StripeWebhookSecret: '',
            StripePriceId: '',
            StripeUnitPrice: 1,
            StripeMinTopUp: 1,
            StripePromotionCodesEnabled: false,
            CreemApiKey: '',
            CreemWebhookSecret: '',
            CreemTestMode: false,
            CreemProducts: '[]',
          }}
          waffoDefaultValues={{
            WaffoEnabled: false,
            WaffoApiKey: '',
            WaffoPrivateKey: '',
            WaffoPublicCert: '',
            WaffoSandboxPublicCert: '',
            WaffoSandboxApiKey: '',
            WaffoSandboxPrivateKey: '',
            WaffoSandbox: true,
            WaffoMerchantId: '',
            WaffoCurrency: 'USD',
            WaffoUnitPrice: 1,
            WaffoMinTopUp: 1,
            WaffoNotifyUrl: '',
            WaffoReturnUrl: '',
            WaffoPayMethods: '[]',
          }}
          waffoPancakeDefaultValues={{
            WaffoPancakeMerchantID: '',
            WaffoPancakePrivateKey: '',
            WaffoPancakeReturnURL: '',
          }}
          complianceDefaults={{
            confirmed: true,
            termsVersion: 'v1',
            confirmedAt: 1,
            confirmedBy: 1,
          }}
        />
      </SettingsPageProvider>
    </QueryClientProvider>
  )
  return {
    cleanup: () => {
      view.unmount()
      queryClient.clear()
      actions.remove()
    },
  }
}

it('saves the complete Alipay configuration once and clears write-only key inputs after success', async () => {
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true, data: [] } })
  const view = renderSettings()
  try {
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Alipay' }))
    const privateKey = screen.getByLabelText('Alipay application private key')
    const publicKey = screen.getByLabelText('Alipay verification public key')
    expect(privateKey).toHaveAttribute('type', 'password')
    expect(privateKey).toHaveValue('')
    await user.type(privateKey, 'test-new-private-key')
    await user.type(publicKey, 'test-new-public-key')
    await user.click(screen.getByRole('button', { name: 'Save all settings' }))
    await waitFor(() => expect(put).toHaveBeenCalledTimes(1))
    expect(put).toHaveBeenCalledWith(
      '/api/option/alipay',
      expect.objectContaining({
        enabled: false,
        sandbox: true,
        appId: 'stored-app',
        sellerId: 'stored-seller',
        privateKey: 'test-new-private-key',
        publicKey: 'test-new-public-key',
        cnyPerUsd: '7.2',
      })
    )
    await waitFor(() => expect(privateKey).toHaveValue(''))
    expect(publicKey).toHaveValue('')
  } finally {
    view.cleanup()
  }
})

it('submits blank keys to preserve stored values when changing the conversion rate', async () => {
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true, data: [] } })
  const view = renderSettings()
  try {
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Alipay' }))
    const rate = screen.getByLabelText('Alipay CNY per USD')
    await user.clear(rate)
    await user.type(rate, '7.3')
    await user.click(screen.getByRole('button', { name: 'Save all settings' }))
    await waitFor(() => expect(put).toHaveBeenCalledTimes(1))
    expect(put).toHaveBeenCalledWith(
      '/api/option/alipay',
      expect.objectContaining({
        privateKey: '',
        publicKey: '',
        cnyPerUsd: '7.3',
      })
    )
  } finally {
    view.cleanup()
  }
})

it('preserves PEM line breaks when pasting a key into the masked field', async () => {
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true, data: [] } })
  const view = renderSettings()
  try {
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Alipay' }))
    const key =
      '-----BEGIN PRIVATE KEY-----\nsynthetic-test-payload\n-----END PRIVATE KEY-----'
    await user.click(screen.getByLabelText('Alipay application private key'))
    await user.paste(key)
    await user.click(screen.getByRole('button', { name: 'Save all settings' }))
    await waitFor(() => expect(put).toHaveBeenCalledTimes(1))
    expect(put).toHaveBeenCalledWith(
      '/api/option/alipay',
      expect.objectContaining({ privateKey: key })
    )
  } finally {
    view.cleanup()
  }
})

it('keeps entered keys for an explicit retry when saving fails', async () => {
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({
      data: { success: false, message: 'Invalid configuration' },
    })
  const view = renderSettings()
  try {
    const user = userEvent.setup()
    await user.click(screen.getByRole('tab', { name: 'Alipay' }))
    const privateKey = screen.getByLabelText('Alipay application private key')
    await user.type(privateKey, 'test-retry-key')
    await user.click(screen.getByRole('button', { name: 'Save all settings' }))
    await waitFor(() => expect(put).toHaveBeenCalledTimes(1))
    expect(privateKey).toHaveValue('test-retry-key')
    expect(screen.getByRole('tab', { name: 'Alipay' })).toHaveAttribute(
      'aria-selected',
      'true'
    )
  } finally {
    view.cleanup()
  }
})
