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
import {
  act,
  render,
  renderHook,
  screen,
  waitFor,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next, { createInstance } from 'i18next'
import type { PropsWithChildren } from 'react'
import { I18nextProvider, initReactI18next, setI18n } from 'react-i18next'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { SubscriptionPurchaseDialog } from '@/features/subscriptions/components/dialogs/subscription-purchase-dialog'
import { formatPlanPrice } from '@/features/subscriptions/lib'
import en from '@/i18n/locales/en.json'
import zh from '@/i18n/locales/zh.json'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { AlipayOrdersCard } from '../components/alipay-orders-card'
import { AlipayCheckoutDialog } from '../components/dialogs/alipay-checkout-dialog'
import { RechargeFormCard } from '../components/recharge-form-card'
import { useAlipayPayment } from '../hooks/use-alipay-payment'
import {
  alipayAppLink,
  formatAlipayAmount,
  isAlipayCheckoutUrl,
  rememberAlipayIntent,
} from '../lib/alipay'

const pending = {
  order_id: 'NALI-test',
  amount_cents: 288,
  status: 'pending',
  qr_code: 'https://qr.alipay.com/test',
}
let client: QueryClient
function Wrapper(props: PropsWithChildren) {
  return (
    <QueryClientProvider client={client}>{props.children}</QueryClientProvider>
  )
}

beforeEach(() => {
  setI18n(i18next)
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  sessionStorage.clear()
  useAuthStore.getState().auth.setUser({ id: 1, username: 'test', role: 1 })
})
afterEach(() => {
  client.clear()
  useAuthStore.getState().auth.reset()
  setI18n(i18next)
  vi.restoreAllMocks()
})

it('reuses the persisted purchase identity after a failed request and page remount', async () => {
  const post = vi
    .spyOn(api, 'post')
    .mockRejectedValueOnce(new Error('network unavailable'))
    .mockResolvedValueOnce({ data: { success: true, data: pending } })
  const first = renderHook(() => useAlipayPayment(), { wrapper: Wrapper })
  await act(async () => {
    expect(await first.result.current.start({ amount: 1 })).toBe(false)
  })
  first.unmount()
  const second = renderHook(() => useAlipayPayment(), { wrapper: Wrapper })
  await act(async () => {
    expect(await second.result.current.start({ amount: 1 })).toBe(true)
  })
  expect((post.mock.calls[0][1] as { request_id: string }).request_id).toBe(
    (post.mock.calls[1][1] as { request_id: string }).request_id
  )
  expect(second.result.current.checkout?.order_id).toBe(pending.order_id)
})

it('keeps identities separate for accounts, wallet amounts and plans', () => {
  const wallet = rememberAlipayIntent(1, { amount: 1 })
  expect(rememberAlipayIntent(1, { amount: 1 })).toBe(wallet)
  expect(rememberAlipayIntent(2, { amount: 1 })).not.toBe(wallet)
  expect(rememberAlipayIntent(1, { amount: 2 })).not.toBe(wallet)
  expect(rememberAlipayIntent(1, { plan_id: 1 })).not.toBe(wallet)
})

it('does not start a payment when its identity cannot be persisted', async () => {
  const post = vi.spyOn(api, 'post')
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
    throw new Error('storage unavailable')
  })
  const hook = renderHook(() => useAlipayPayment(), { wrapper: Wrapper })
  await act(async () => {
    expect(await hook.result.current.start({ amount: 1 })).toBe(false)
  })
  expect(post).not.toHaveBeenCalled()
})

it('rejects business failures and untrusted payment links without showing a checkout', async () => {
  vi.spyOn(api, 'post')
    .mockResolvedValueOnce({ data: { success: false, message: 'rejected' } })
    .mockResolvedValueOnce({
      data: {
        success: true,
        data: { ...pending, qr_code: 'javascript:alert(1)' },
      },
    })
  const hook = renderHook(() => useAlipayPayment(), { wrapper: Wrapper })
  for (const amount of [1, 2]) {
    await act(async () => {
      expect(await hook.result.current.start({ amount })).toBe(false)
    })
    expect(hook.result.current.checkout).toBeNull()
  }
})

it('marks a payment received only after a successful owner query', async () => {
  vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: pending },
  })
  vi.spyOn(api, 'get')
    .mockRejectedValueOnce(new Error('query unavailable'))
    .mockResolvedValueOnce({
      data: { success: true, data: { ...pending, status: 'success' } },
    })
  const paid = vi.fn()
  const hook = renderHook(() => useAlipayPayment(paid), { wrapper: Wrapper })
  await act(async () => {
    await hook.result.current.start({ amount: 1 })
  })
  act(() => {
    hook.result.current.check()
  })
  await waitFor(() => expect(hook.result.current.checking).toBe(false))
  expect(hook.result.current.checkout?.status).toBe('pending')
  expect(paid).not.toHaveBeenCalled()
  act(() => {
    hook.result.current.check()
  })
  await waitFor(() =>
    expect(hook.result.current.checkout?.status).toBe('success')
  )
  await waitFor(() => expect(paid).toHaveBeenCalledTimes(1))
})

it('hides an existing checkout as soon as the signed-in account changes', async () => {
  vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: pending },
  })
  const hook = renderHook(() => useAlipayPayment(), { wrapper: Wrapper })
  await act(async () => {
    await hook.result.current.start({ amount: 1 })
  })
  act(() => {
    useAuthStore.getState().auth.setUser({ id: 2, username: 'other', role: 1 })
  })
  expect(hook.result.current.checkout).toBeNull()
})

it('does not reopen a dismissed checkout when its pending query finishes', async () => {
  vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: pending },
  })
  let finish:
    | ((response: { data: { success: boolean; data: typeof pending } }) => void)
    | undefined
  vi.spyOn(api, 'get').mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve
      })
  )
  const hook = renderHook(() => useAlipayPayment(), { wrapper: Wrapper })
  await act(async () => {
    await hook.result.current.start({ amount: 1 })
  })
  act(() => {
    hook.result.current.check()
  })
  await waitFor(() => expect(finish).toBeDefined())
  act(() => {
    hook.result.current.close()
  })
  await act(async () => {
    finish?.({ data: { success: true, data: pending } })
  })
  expect(hook.result.current.checkout).toBeNull()
})

it('renders only QR payloads as a scannable image and uses a link for page checkout', () => {
  const view = render(
    <AlipayCheckoutDialog
      checkout={pending}
      checking={false}
      onCheck={vi.fn()}
      onClose={vi.fn()}
    />
  )
  expect(
    screen.getByRole('img', { name: 'Alipay payment QR code' })
  ).toBeInTheDocument()
  expect(
    screen.getByRole('button', { name: 'Open Alipay checkout' })
  ).toHaveAttribute('href', alipayAppLink(pending.qr_code))
  view.rerender(
    <AlipayCheckoutDialog
      checkout={{
        ...pending,
        qr_code: undefined,
        pay_url: 'https://openapi.alipay.com/gateway.do?test=1',
      }}
      checking
      onCheck={vi.fn()}
      onClose={vi.fn()}
    />
  )
  expect(screen.queryByRole('img')).not.toBeInTheDocument()
  expect(
    screen.getByRole('button', { name: 'Open Alipay checkout' })
  ).toHaveAttribute('href', 'https://openapi.alipay.com/gateway.do?test=1')
  expect(screen.getByRole('button', { name: 'Check payment' })).toBeDisabled()
})

it('updates the checkout language and CNY formatting when the language changes', async () => {
  const i18n = createInstance()
  await i18n.use(initReactI18next).init({
    lng: 'en',
    fallbackLng: false,
    keySeparator: false,
    resources: { en, zhCN: zh },
  })
  render(
    <I18nextProvider i18n={i18n}>
      <AlipayCheckoutDialog
        checkout={pending}
        checking={false}
        onCheck={vi.fn()}
        onClose={vi.fn()}
      />
    </I18nextProvider>
  )
  expect(
    screen.getByRole('heading', { name: 'Alipay face-to-face' })
  ).toBeInTheDocument()
  await act(async () => {
    await i18n.changeLanguage('zhCN')
  })
  expect(
    screen.getByRole('heading', { name: '支付宝当面付' })
  ).toBeInTheDocument()
  expect(screen.getByText('¥2.88')).toBeInTheDocument()
})

it('resumes a stored server order with its original request identity', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: [
        {
          ...pending,
          request_id: 'original-request',
          requested_amount: 1,
          plan_id: 0,
        },
      ],
    },
  })
  const resume = vi.fn().mockResolvedValue(true)
  render(<AlipayOrdersCard onResume={resume} processing={false} />, {
    wrapper: Wrapper,
  })
  await userEvent.click(
    await screen.findByRole('button', { name: 'Resume payment' })
  )
  expect(resume).toHaveBeenCalledWith({
    request_id: 'original-request',
    amount: 1,
  })
})

it('formats CNY cents without exchange conversion in every interface language and fallback', () => {
  for (const language of [
    'zhCN',
    'zhTW',
    'en',
    'fr',
    'ru',
    'ja',
    'vi',
    'invalid_locale',
  ]) {
    expect(formatAlipayAmount(123, language).replaceAll(/\D/g, '')).toBe('123')
    expect(
      formatPlanPrice(
        { price_amount: 1.23, currency: 'CNY' },
        language
      ).replaceAll(/\D/g, '')
    ).toBe('123')
    expect(
      formatPlanPrice(
        { price_amount: 1.23, currency: 'USD' },
        language
      ).replaceAll(/\D/g, '')
    ).toBe('123')
  }
  expect(isAlipayCheckoutUrl('https://qr.alipay.com/test')).toBe(true)
  for (const raw of [
    'javascript:alert(1)',
    'https://alipay.com.attacker.example',
    'https://user:password@qr.alipay.com/test',
    'http://qr.alipay.com/test',
  ]) {
    expect(isAlipayCheckoutUrl(raw)).toBe(false)
  }
})

it('keeps an uncertain cancellation in closing and confirms closure through an owner query', async () => {
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValueOnce({ data: { success: true, data: pending } })
    .mockResolvedValueOnce({
      data: { success: true, data: { ...pending, status: 'closing' } },
    })
  vi.spyOn(api, 'get').mockResolvedValueOnce({
    data: { success: true, data: { ...pending, status: 'closed' } },
  })
  const paid = vi.fn()
  const hook = renderHook(() => useAlipayPayment(paid), { wrapper: Wrapper })
  await act(async () => {
    await hook.result.current.start({ amount: 1 })
  })
  act(() => {
    hook.result.current.cancel()
  })
  await waitFor(() =>
    expect(hook.result.current.checkout?.status).toBe('closing')
  )
  expect(post).toHaveBeenLastCalledWith(
    '/api/user/alipay/orders/NALI-test/close',
    {}
  )
  expect(paid).not.toHaveBeenCalled()
  act(() => {
    hook.result.current.check()
  })
  await waitFor(() =>
    expect(hook.result.current.checkout?.status).toBe('closed')
  )
  expect(paid).not.toHaveBeenCalled()
})

it('preserves the pending order and request identity when the cancel request fails', async () => {
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValueOnce({ data: { success: true, data: pending } })
    .mockRejectedValueOnce(new Error('network unavailable'))
  const hook = renderHook(() => useAlipayPayment(), { wrapper: Wrapper })
  await act(async () => {
    await hook.result.current.start({ amount: 1 })
  })
  const identity = (post.mock.calls[0][1] as { request_id: string }).request_id
  act(() => {
    hook.result.current.cancel()
  })
  await waitFor(() => expect(post).toHaveBeenCalledTimes(2))
  await waitFor(() => expect(hook.result.current.checking).toBe(false))
  expect(hook.result.current.checkout?.status).toBe('pending')
  expect(rememberAlipayIntent(1, { amount: 1 })).toBe(identity)
})

it('credits the UI when payment won the cancellation race without calling it closed', async () => {
  vi.spyOn(api, 'post')
    .mockResolvedValueOnce({ data: { success: true, data: pending } })
    .mockResolvedValueOnce({
      data: { success: true, data: { ...pending, status: 'success' } },
    })
  const paid = vi.fn()
  const hook = renderHook(() => useAlipayPayment(paid), { wrapper: Wrapper })
  await act(async () => {
    await hook.result.current.start({ amount: 1 })
  })
  act(() => {
    hook.result.current.cancel()
  })
  await waitFor(() =>
    expect(hook.result.current.checkout?.status).toBe('success')
  )
  await waitFor(() => expect(paid).toHaveBeenCalledTimes(1))
})

it('allows a new purchase identity after recovery reports that the previous order is closed', async () => {
  const oldId = rememberAlipayIntent(1, { amount: 1 })
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValueOnce({
      data: {
        success: true,
        data: { ...pending, status: 'closed', qr_code: undefined },
      },
    })
    .mockResolvedValueOnce({
      data: { success: true, data: { ...pending, order_id: 'new-order' } },
    })
  const hook = renderHook(() => useAlipayPayment(), { wrapper: Wrapper })
  await act(async () => {
    expect(await hook.result.current.start({ amount: 1 })).toBe(true)
  })
  expect(hook.result.current.checkout?.status).toBe('closed')
  await act(async () => {
    await hook.result.current.start({ amount: 1 })
  })
  expect((post.mock.calls[0][1] as { request_id: string }).request_id).toBe(
    oldId
  )
  expect((post.mock.calls[1][1] as { request_id: string }).request_id).not.toBe(
    oldId
  )
})

it('requires confirmation before cancelling and hides payment links while closure is uncertain', async () => {
  const cancel = vi.fn()
  const view = render(
    <AlipayCheckoutDialog
      checkout={pending}
      checking={false}
      onCheck={vi.fn()}
      onClose={vi.fn()}
      onCancel={cancel}
    />
  )
  await userEvent.click(screen.getByRole('button', { name: 'Cancel payment' }))
  expect(cancel).not.toHaveBeenCalled()
  await userEvent.click(
    screen.getByRole('button', { name: 'Close unpaid order' })
  )
  expect(cancel).toHaveBeenCalledTimes(1)
  view.rerender(
    <AlipayCheckoutDialog
      checkout={{ ...pending, status: 'closing' }}
      checking={false}
      onCheck={vi.fn()}
      onClose={vi.fn()}
      onCancel={cancel}
    />
  )
  expect(
    screen.queryByRole('img', { name: 'Alipay payment QR code' })
  ).not.toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'Open Alipay checkout' })
  ).not.toBeInTheDocument()
  expect(screen.getByRole('status')).toHaveTextContent(
    'Payment closure is awaiting confirmation. Check again.'
  )
  view.rerender(
    <AlipayCheckoutDialog
      checkout={{ ...pending, status: 'closed' }}
      checking={false}
      onCheck={vi.fn()}
      onClose={vi.fn()}
      onCancel={cancel}
    />
  )
  expect(screen.getByRole('status')).toHaveTextContent('Payment closed')
  expect(
    screen.queryByRole('button', { name: 'Check payment' })
  ).not.toBeInTheDocument()
})

it('keeps historical closing orders recoverable even when new Alipay checkout is disabled', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: [
        {
          ...pending,
          status: 'closing',
          request_id: 'closing-request',
          requested_amount: 1,
          plan_id: 0,
        },
      ],
    },
  })
  const resume = vi.fn().mockResolvedValue(true)
  render(
    <AlipayOrdersCard enabled={false} onResume={resume} processing={false} />,
    { wrapper: Wrapper }
  )
  await userEvent.click(
    await screen.findByRole('button', { name: 'Check payment' })
  )
  expect(resume).toHaveBeenCalledWith({
    request_id: 'closing-request',
    amount: 1,
  })
})

it('does not regress a confirmed closure when an older pending query finishes later', async () => {
  vi.spyOn(api, 'post')
    .mockResolvedValueOnce({ data: { success: true, data: pending } })
    .mockResolvedValueOnce({
      data: { success: true, data: { ...pending, status: 'closed' } },
    })
  let finish:
    | ((response: { data: { success: boolean; data: typeof pending } }) => void)
    | undefined
  vi.spyOn(api, 'get').mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve
      })
  )
  const hook = renderHook(() => useAlipayPayment(), { wrapper: Wrapper })
  await act(async () => {
    await hook.result.current.start({ amount: 1 })
  })
  act(() => {
    hook.result.current.check()
  })
  await waitFor(() => expect(finish).toBeDefined())
  act(() => {
    hook.result.current.cancel()
  })
  await waitFor(() =>
    expect(hook.result.current.checkout?.status).toBe('closed')
  )
  await act(async () => {
    finish?.({ data: { success: true, data: pending } })
  })
  expect(hook.result.current.checkout?.status).toBe('closed')
})

it('offers direct Alipay when Epay is disabled', async () => {
  const select = vi.fn()
  render(
    <RechargeFormCard
      topupInfo={{
        enable_online_topup: false,
        enable_alipay_topup: true,
        enable_stripe_topup: false,
        pay_methods: [{ type: 'alipay_direct', name: 'Alipay' }],
        min_topup: 1,
        stripe_min_topup: 1,
        amount_options: [1],
        discount: {},
      }}
      presetAmounts={[]}
      selectedPreset={null}
      topupAmount={1}
      paymentAmount={2.88}
      calculating={false}
      paymentLoading={null}
      redemptionCode=''
      redeeming={false}
      onSelectPreset={vi.fn()}
      onTopupAmountChange={vi.fn()}
      onPaymentMethodSelect={select}
      onRedemptionCodeChange={vi.fn()}
      onRedeem={vi.fn()}
    />
  )
  await userEvent.click(screen.getByRole('button', { name: /Alipay/ }))
  expect(select).toHaveBeenCalledWith(
    expect.objectContaining({ type: 'alipay_direct' })
  )
})

it('starts a plan checkout through the direct Alipay endpoint', async () => {
  const post = vi
    .spyOn(api, 'post')
    .mockResolvedValue({ data: { success: true, data: pending } })
  render(
    <SubscriptionPurchaseDialog
      open
      onOpenChange={vi.fn()}
      enableAlipay
      enableOnlineTopUp={false}
      plan={{
        plan: {
          id: 8,
          title: 'Plan',
          price_amount: 2.88,
          currency: 'CNY',
          duration_unit: 'month',
          duration_value: 1,
          quota_reset_period: 'never',
          enabled: true,
          sort_order: 0,
          allow_balance_pay: false,
          allow_wallet_overflow: false,
          max_purchase_per_user: 0,
          total_amount: 1600,
        },
      }}
    />,
    { wrapper: Wrapper }
  )
  expect(screen.getByText('CN¥2.88')).toBeInTheDocument()
  await userEvent.click(
    screen.getByRole('button', { name: 'Alipay face-to-face' })
  )
  await screen.findByRole('img', { name: 'Alipay payment QR code' })
  expect(post).toHaveBeenCalledWith(
    '/api/subscription/alipay/pay',
    expect.objectContaining({ plan_id: 8, request_id: expect.any(String) })
  )
})
