import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { QRCodeSVG } from 'qrcode.react'
import { Button } from '@/components/ui/button'
import { getUserBillingHistory, isApiSuccess } from '../api'
import type { AlipayCheckoutData } from '../hooks/use-payment'

export function AlipayCheckoutCard({ order, onPaid, onClose }: {
  order: AlipayCheckoutData
  onPaid: () => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [status, setStatus] = useState('pending')
  const [unavailable, setUnavailable] = useState(false)

  useEffect(() => {
    let active = true
    let timer: ReturnType<typeof setTimeout>
    setStatus('pending')
    setUnavailable(false)
    const check = async () => {
      let terminal = false
      try {
        const response = await getUserBillingHistory(1, 10, order.tradeNo)
        if (!active) return
        if (!isApiSuccess(response)) throw new Error('Status unavailable')
        const saved = response.data?.items.find(item => item.trade_no === order.tradeNo)
        setUnavailable(!saved)
        if (saved) {
          setStatus(saved.status)
          terminal = saved.status === 'success' || saved.status === 'expired'
          if (saved.status === 'success') onPaid()
        }
      } catch {
        if (active) setUnavailable(true)
      } finally {
        if (active && !terminal) timer = setTimeout(check, 5000)
      }
    }
    void check()
    return () => { active = false; clearTimeout(timer) }
  }, [order.tradeNo, onPaid])

  let statusMessage = t('Waiting for payment confirmation...')
  if (status === 'success') {
    statusMessage = t('Payment confirmed. Balance updated.')
  } else if (status === 'expired') {
    statusMessage = t('Payment order expired')
  } else if (unavailable) {
    statusMessage = t('Unable to check payment status. Retrying automatically; do not pay again.')
  }

  return (
    <section className='bg-card rounded-xl border p-6 text-center space-y-5' aria-label={t('Alipay QR payment')}>
      <h3 className='text-xl font-semibold'>{t('Alipay QR payment')}</h3>
      <p>{t('Payment amount')}: ¥{order.amount}</p>
      {status === 'pending' && <>
        <div className='mx-auto w-fit rounded-xl border-2 border-sky-500 bg-white p-4'>
          <QRCodeSVG value={order.qrCode} size={256} level='M' title={t('Alipay QR payment')} />
        </div>
        <p className='text-muted-foreground'>{t('Scan with Alipay. This page updates automatically after payment.')}</p>
      </>}
      <p role='status'>{statusMessage}</p>
      <p className='break-all text-xs text-muted-foreground'>{t('Order number')}: {order.tradeNo}</p>
      <Button variant='outline' onClick={onClose}>{t('Return to wallet')}</Button>
      <p className='text-xs text-muted-foreground'>{t('Returning does not cancel this order. Check order history before paying again.')}</p>
    </section>
  )
}
