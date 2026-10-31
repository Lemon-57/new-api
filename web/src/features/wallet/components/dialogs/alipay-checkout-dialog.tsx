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
import { useTranslation } from 'react-i18next'

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
}

export function AlipayCheckoutDialog(props: Props) {
  const { t, i18n } = useTranslation()
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
  return (
    <Dialog
      open={!!order}
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
      title={t('Alipay face-to-face')}
      description={t('Scan with Alipay to pay. Then check the payment status.')}
      contentClassName='sm:max-w-md'
      footer={
        <>
          <Button variant='outline' onClick={props.onClose}>
            {t('Close')}
          </Button>
          {!paid && (
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
          <p role='status'>
            {paid ? t('Payment received') : t('Payment pending')}
          </p>
          {!paid && qr && (
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
          {!paid && appUrl && (
            <Button variant='outline' render={<a href={appUrl} />}>
              {t('Open Alipay checkout')}
            </Button>
          )}
          {!paid && !qr && payUrl && (
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
          {!paid && (
            <p className='text-muted-foreground text-xs'>
              {t('Closing this window keeps the order available for recovery.')}
            </p>
          )}
        </div>
      )}
    </Dialog>
  )
}
