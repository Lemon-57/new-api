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
import { useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import { TitledCard } from '@/components/ui/titled-card'
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import {
  formatAlipayAmount,
  type AlipayIntent,
  type AlipayOrder,
} from '../lib/alipay'
import type { ApiResponse } from '../types'

interface Props {
  onResume: (intent: AlipayIntent) => Promise<boolean>
  processing: boolean
  enabled?: boolean
}

export function AlipayOrdersCard(props: Props) {
  const { t, i18n } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const statusLabel = (status: string): string => {
    if (status === 'success') return t('Payment received')
    if (status === 'closed') return t('Payment closed')
    if (status === 'closing') {
      return t('Payment closure is awaiting confirmation. Check again.')
    }
    return t('Payment pending')
  }
  const orders = useQuery({
    queryKey: ['alipay-orders', userId],
    enabled: !!userId,
    queryFn: async () =>
      requireServerSuccess(
        (await api.get<ApiResponse<AlipayOrder[]>>('/api/user/alipay/orders'))
          .data
      ).data || [],
  })
  if (props.enabled === false && orders.isSuccess && orders.data.length === 0) {
    return null
  }
  let content: ReactNode
  if (orders.isPending) content = <LoadingState />
  else if (orders.isError) {
    content = (
      <ErrorState
        onRetry={() => {
          void orders.refetch()
        }}
      />
    )
  } else {
    content = orders.data?.map((order) => (
      <div
        key={order.order_id}
        className='flex flex-wrap items-center justify-between gap-3 rounded-lg border p-3'
      >
        <div className='min-w-0 space-y-1'>
          <p className='font-mono text-xs break-all'>{order.order_id}</p>
          <p className='text-sm'>
            {formatAlipayAmount(
              order.amount_cents,
              i18n.resolvedLanguage || i18n.language
            )}{' '}
            · {statusLabel(order.status)}
          </p>
        </div>
        {(order.status === 'pending' || order.status === 'closing') && (
          <Button
            variant='outline'
            disabled={props.processing}
            onClick={() => {
              void props.onResume({
                request_id: order.request_id,
                ...(order.plan_id
                  ? { plan_id: order.plan_id }
                  : { amount: order.requested_amount }),
              })
            }}
          >
            {order.status === 'closing'
              ? t('Check payment')
              : t('Resume payment')}
          </Button>
        )}
      </div>
    ))
  }
  return (
    <TitledCard title={t('Recent Alipay orders')}>
      <div className='space-y-3 px-4 pb-4'>
        <Button
          variant='outline'
          size='sm'
          onClick={() => {
            void orders.refetch()
          }}
          disabled={orders.isFetching}
        >
          {t('Refresh')}
        </Button>
        {content}
        {orders.data?.length === 0 && (
          <p className='text-muted-foreground text-sm'>
            {t('No records found')}
          </p>
        )}
      </div>
    </TitledCard>
  )
}
