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
import { QRCodeSVG } from 'qrcode.react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { CopyButton } from '@/components/copy-button'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'

import {
  alipayAppLink,
  formatAlipayAmount,
  isAlipayCheckoutUrl,
  type AlipayCheckout,
} from '../../lib/alipay'

interface Props {
  checkout: AlipayCheckout | null
  checking: boolean
  onCheck: () => void
  onClose: () => void
  onCancel?: () => void
}

export function AlipayCheckoutDialog(props: Props) {
  const { t, i18n } = useTranslation()
  const [confirmationOrder, setConfirmationOrder] = useState<string | null>(
    null
  )
  const order = props.checkout
  const qr =
    order?.qr_code && isAlipayCheckoutUrl(order.qr_code)
      ? order.qr_code
      : undefined
  const payUrl =
    order?.pay_url && isAlipayCheckoutUrl(order.pay_url)
      ? order.pay_url
      : undefined
  const appUrl = qr ? alipayAppLink(qr) : undefined
  const paid = order?.status === 'success'
  const pending = order?.status === 'pending'
  const closing = order?.status === 'closing'
  const closed = order?.status === 'closed'
  let statusLabel = t('Payment pending')
  if (paid) statusLabel = t('Payment received')
  else if (closing) {
    statusLabel = t('Payment closure is awaiting confirmation. Check again.')
  } else if (closed) statusLabel = t('Payment closed')
  const dismiss = () => {
    setConfirmationOrder(null)
    props.onClose()
  }
  return (
    <>
      <Dialog
        open={!!order}
        onOpenChange={(open) => {
          if (!open) dismiss()
        }}
        title={t('Alipay face-to-face')}
        description={t(
          'Scan with Alipay to pay. Then check the payment status.'
        )}
        contentClassName='sm:max-w-md'
        footer={
          <>
            <Button variant='outline' onClick={dismiss}>
              {t('Close')}
            </Button>
            {pending && props.onCancel && (
              <Button
                variant='destructive'
                disabled={props.checking}
                onClick={() => setConfirmationOrder(order?.order_id || null)}
              >
                {t('Cancel payment')}
              </Button>
            )}
            {(pending || closing) && (
              <Button onClick={props.onCheck} disabled={props.checking}>
                {t('Check payment')}
              </Button>
            )}
          </>
        }
      >
        {order && (
          <div className='space-y-4 text-center'>
            <p className='text-2xl font-semibold'>
              {formatAlipayAmount(
                order.amount_cents,
                i18n.resolvedLanguage || i18n.language
              )}
            </p>
            <p role='status'>{statusLabel}</p>
            {pending && qr && (
              <div className='mx-auto w-fit rounded-lg bg-white p-3'>
                <QRCodeSVG
                  value={qr}
                  size={224}
                  role='img'
                  aria-label={t('Alipay payment QR code')}
                  marginSize={4}
                />
              </div>
            )}
            {pending && appUrl && (
              <Button variant='outline' render={<a href={appUrl} />}>
                {t('Open Alipay checkout')}
              </Button>
            )}
            {pending && !qr && payUrl && (
              <Button
                render={
                  <a href={payUrl} target='_blank' rel='noopener noreferrer' />
                }
              >
                {t('Open Alipay checkout')}
              </Button>
            )}
            <div className='flex items-center justify-center gap-2'>
              <span className='font-mono text-xs break-all'>
                {order.order_id}
              </span>
              <CopyButton
                value={order.order_id}
                aria-label={t('Copy order number')}
              />
            </div>
            {(pending || closing) && (
              <p className='text-muted-foreground text-xs'>
                {t(
                  'Closing this window keeps the order available for recovery.'
                )}
              </p>
            )}
          </div>
        )}
      </Dialog>
      <ConfirmDialog
        open={!!order && pending && confirmationOrder === order.order_id}
        onOpenChange={(open) => {
          if (!open) setConfirmationOrder(null)
        }}
        title={t('Cancel payment')}
        desc={t(
          'Close this unpaid order? If payment has already succeeded, it will be credited instead.'
        )}
        confirmText={t('Close unpaid order')}
        disabled={props.checking}
        destructive
        handleConfirm={() => {
          setConfirmationOrder(null)
          props.onCancel?.()
        }}
      />
    </>
  )
}
