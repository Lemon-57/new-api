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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { api } from '@/lib/api'
import { handleServerError } from '@/lib/handle-server-error'
import {
  createServerError,
  requireServerSuccess,
} from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import {
  alipayIntentKey,
  isAlipayCheckoutUrl,
  rememberAlipayIntent,
  type AlipayCheckout,
  type AlipayIntent,
  type AlipayOrder,
} from '../lib/alipay'
import type { ApiResponse } from '../types'

export function useAlipayPayment(onPaid?: () => void | Promise<void>) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const generation = useRef(0)
  const [state, setState] = useState<{
    actor: number
    data: AlipayCheckout
    intent: AlipayIntent
    operation: number
  } | null>(null)
  const ownedState = state && state.actor === userId ? state : null
  const checkout = ownedState?.data ?? null
  const currentIntent = ownedState?.intent ?? null

  const received = (
    data: AlipayCheckout,
    actor: number,
    intent: AlipayIntent,
    operation: number
  ) => {
    if (
      useAuthStore.getState().auth.user?.id !== actor ||
      operation !== generation.current
    ) {
      return false
    }
    setState({ actor, data, intent, operation })
    if (data.status === 'success') {
      const key = alipayIntentKey(actor, intent)
      try {
        if (sessionStorage.getItem(key) === intent.request_id) {
          sessionStorage.removeItem(key)
        }
      } catch {
        /* Payment is confirmed even if storage cleanup fails. */
      }
      void queryClient.invalidateQueries({ queryKey: ['alipay-orders', actor] })
      void Promise.resolve()
        .then(() => onPaid?.())
        .catch(handleServerError)
    }
    return true
  }

  const startMutation = useMutation({
    mutationFn: async (intent: AlipayIntent) => {
      const operation = ++generation.current
      const actor = useAuthStore.getState().auth.user?.id
      if (!actor) throw new Error(t('Please sign in again.'))
      let requestId: string
      try {
        requestId = rememberAlipayIntent(actor, intent)
      } catch {
        throw new Error(
          t('Payment was not started because browser storage is unavailable.')
        )
      }
      const mobile = window.matchMedia?.('(pointer: coarse)').matches === true
      const request = {
        ...intent,
        request_id: requestId,
        is_mobile: mobile,
        mobile_precreate: mobile,
      }
      const endpoint = intent.plan_id
        ? '/api/subscription/alipay/pay'
        : '/api/user/alipay/pay'
      const response = requireServerSuccess(
        (await api.post<ApiResponse<AlipayCheckout>>(endpoint, request)).data
      )
      const data = response.data
      if (
        !response.success ||
        !data ||
        !data.order_id ||
        !Number.isSafeInteger(data.amount_cents) ||
        data.amount_cents <= 0 ||
        !['pending', 'success'].includes(data.status)
      ) {
        throw createServerError(response, t('Payment request failed'))
      }
      if (
        data.status !== 'success' &&
        (!data.qr_code || !isAlipayCheckoutUrl(data.qr_code)) &&
        (!data.pay_url || !isAlipayCheckoutUrl(data.pay_url))
      ) {
        throw new Error(t('Invalid payment redirect URL'))
      }
      return {
        data,
        actor,
        intent: { ...intent, request_id: requestId },
        operation,
      }
    },
    onSuccess: ({ data, actor, intent, operation }) => {
      received(data, actor, intent, operation)
    },
    onError: (error) => handleServerError(error, t('Payment request failed')),
  })

  const checkMutation = useMutation({
    mutationFn: async () => {
      if (!checkout || !currentIntent || !userId || !ownedState) {
        throw new Error(t('Invalid parameters'))
      }
      const response = requireServerSuccess(
        (
          await api.get<ApiResponse<AlipayOrder>>(
            `/api/user/alipay/orders/${encodeURIComponent(checkout.order_id)}`
          )
        ).data
      )
      if (
        !response.success ||
        !response.data ||
        response.data.order_id !== checkout.order_id ||
        response.data.amount_cents !== checkout.amount_cents ||
        !['pending', 'success'].includes(response.data.status)
      ) {
        throw createServerError(response)
      }
      return {
        data: { ...checkout, ...response.data },
        actor: userId,
        intent: currentIntent,
        operation: ownedState.operation,
      }
    },
    onSuccess: ({ data, actor, intent, operation }) => {
      received(data, actor, intent, operation)
    },
    onError: (error) =>
      handleServerError(error, t('Payment status could not be verified.')),
  })

  return {
    checkout,
    processing: startMutation.isPending,
    checking: checkMutation.isPending,
    close: () => {
      generation.current++
      setState(null)
    },
    check: () => checkMutation.mutate(),
    start: async (intent: AlipayIntent): Promise<boolean> => {
      try {
        const result = await startMutation.mutateAsync(intent)
        return (
          useAuthStore.getState().auth.user?.id === result.actor &&
          result.operation === generation.current
        )
      } catch {
        return false
      }
    },
  }
}
