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
import { toIntlLocale } from '@/i18n/languages'

export interface AlipayCheckout {
  order_id: string
  status: string
  amount_cents: number
  qr_code?: string
  pay_url?: string
}

export interface AlipayOrder extends AlipayCheckout {
  request_id: string
  requested_amount: number
  plan_id: number
}

export interface AlipayIntent {
  amount?: number
  plan_id?: number
  request_id?: string
}

// Payment URLs are intentionally limited to Alipay, including its sandbox.
// They never accept credentials, script URLs or look-alike hostname suffixes.
export function isAlipayCheckoutUrl(raw: string): boolean {
  try {
    const url = new URL(raw)
    return (
      url.protocol === 'https:' &&
      !url.username &&
      !url.password &&
      ['alipay.com', 'alipaydev.com'].some(
        (host) => url.hostname === host || url.hostname.endsWith(`.${host}`)
      )
    )
  } catch {
    return false
  }
}

export function alipayAppLink(qrCode: string): string | undefined {
  if (!isAlipayCheckoutUrl(qrCode)) return undefined
  return `alipays://platformapi/startapp?appId=20000067&url=${encodeURIComponent(qrCode)}`
}

// The shared currency helper follows the site's display currency. This payment
// is already in CNY cents, so it must explicitly format CNY without conversion.
export function formatAlipayAmount(cents: number, language: string): string {
  return new Intl.NumberFormat(toIntlLocale(language), {
    style: 'currency',
    currency: 'CNY',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(cents / 100)
}

export function alipayIntentKey(userId: number, intent: AlipayIntent): string {
  return `alipay-intent:${userId}:${intent.plan_id ? `plan:${intent.plan_id}` : `wallet:${intent.amount}`}`
}

export function rememberAlipayIntent(
  userId: number,
  intent: AlipayIntent
): string {
  const key = alipayIntentKey(userId, intent)
  const previous = sessionStorage.getItem(key)
  const id = intent.request_id || previous || crypto.randomUUID()
  if (!/^[A-Za-z0-9_-]{1,64}$/.test(id)) throw new Error('Invalid parameters')
  // Persist BEFORE the request. Losing its HTTP reply must not mint a new ID.
  sessionStorage.setItem(key, id)
  return id
}
