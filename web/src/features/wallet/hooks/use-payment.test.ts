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
import { describe, expect, test, vi } from 'vitest'
import { api } from '@/lib/api'

import { PAYMENT_TYPES } from '../constants'
import { requestPaymentAmount, usePayment } from './use-payment'
import { renderHook, act, render, waitFor, cleanup } from '@testing-library/react'
import { createElement } from 'react'
import { AlipayCheckoutCard } from '../components/alipay-checkout-card'

vi.mock('@/lib/api', () => ({ api: { post: vi.fn(), get: vi.fn() } }))

describe('payment amount routing', () => {
  test('uses the dedicated Waffo amount calculator', async () => {
    const calls: string[] = []
    const amount = await requestPaymentAmount(120, PAYMENT_TYPES.WAFFO, {
      regular: async () => {
        calls.push('regular')
        return { success: true, data: '1' }
      },
      stripe: async () => {
        calls.push('stripe')
        return { success: true, data: '2' }
      },
      waffo: async (request) => {
        calls.push(`waffo:${request.amount}`)
        return { success: true, data: '18.75' }
      },
      waffoPancake: async () => {
        calls.push('pancake')
        return { success: true, data: '4' }
      },
    })

    expect(amount).toBe(18.75)
    expect(calls).toEqual(['waffo:120'])
  })
})


test('keeps Alipay QR checkout in the wallet without a popup or document navigation', async () => {
  const open = vi.spyOn(window, 'open')
  const { result } = renderHook(() => usePayment())
  vi.mocked(api.post).mockResolvedValueOnce({ data: { message: 'success', url: '/api/user/alipay/checkout', data: { trade_no: 'synthetic', qr_code: 'https://qr.alipay.com/test', amount: '10.00' } } })
  await act(async () => { expect(await result.current.processPayment(10, 'alipay')).toBe(true) })
  expect(result.current.alipayCheckout).toEqual({ tradeNo: 'synthetic', qrCode: 'https://qr.alipay.com/test', amount: '10.00' })
  expect(open).not.toHaveBeenCalled()
  expect(api.post).toHaveBeenCalledTimes(1)
})


test('in-page QR confirms only the matching order and refreshes balance', async () => {
  const paid = vi.fn()
  vi.mocked(api.get).mockResolvedValue({ data: { success: true, data: { items: [{ trade_no: 'original', status: 'success' }] } } })
  const view = render(createElement(AlipayCheckoutCard, { order: { tradeNo: 'original', qrCode: 'https://qr.alipay.com/test', amount: '10.00' }, onPaid: paid, onClose: vi.fn() }))
  await waitFor(() => expect(view.getByRole('status').textContent).toContain('Payment confirmed. Balance updated.'))
  expect(paid).toHaveBeenCalledTimes(1)
  expect(view.container.querySelector('svg[role="img"]')).toBeNull()
  cleanup()
})

test('another successful order never confirms the displayed QR order', async () => {
  const paid = vi.fn()
  vi.mocked(api.get).mockResolvedValue({ data: { success: true, data: { items: [{ trade_no: 'other', status: 'success' }] } } })
  const view = render(createElement(AlipayCheckoutCard, { order: { tradeNo: 'original', qrCode: 'https://qr.alipay.com/test', amount: '10.00' }, onPaid: paid, onClose: vi.fn() }))
  await waitFor(() => expect(view.getByRole('status').textContent).toContain('Retrying automatically'))
  expect(paid).not.toHaveBeenCalled()
  cleanup()
})
