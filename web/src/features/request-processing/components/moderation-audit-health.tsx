import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import type { ModerationStatus } from '../moderation-audit-api'

export function ModerationAuditHealth(props: { status: ModerationStatus }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const time = new Intl.DateTimeFormat(locale, {
    dateStyle: 'medium',
    timeStyle: 'short',
  })
  const resultNames: Record<string, string> = {
    not_checked: t('Not checked'),
    ok: t('Available'),
    http_error: t('HTTP error'),
    timeout: t('Timeout'),
    transport_error: t('Connection error'),
    invalid_response: t('Invalid response'),
  }
  const queueNames: Record<string, string> = {
    observations: t('Background audits'),
    records: t('Audit records'),
    notifications: t('Email notifications'),
  }
  return (
    <section
      className='space-y-3 rounded-md border p-4'
      aria-label={t('Audit service status')}
    >
      <h2 className='text-sm font-semibold'>
        {t('Audit service status')} · {props.status.node || '—'}
        {props.status.version && ` · ${props.status.version}`}
      </h2>
      <div className='grid gap-3 sm:grid-cols-3'>
        {Object.entries(props.status.queues).map(([name, queue]) => (
          <div key={name} className='text-sm'>
            <div className='font-medium'>{queueNames[name] ?? name}</div>
            <div className='text-muted-foreground'>
              {t('Active')}: {formatNumber(queue.active, locale)} ·{' '}
              {t('Queued')}: {formatNumber(queue.pending, locale)} ·{' '}
              {t('Dropped')}: {formatNumber(queue.dropped, locale)}
            </div>
          </div>
        ))}
      </div>
      <div className='space-y-2'>
        {props.status.instances.map((item) => (
          <div
            key={item.instance_id}
            className='flex flex-wrap items-center gap-2 text-sm'
          >
            <span className='font-medium'>
              {item.name} · {item.engine}
            </span>
            {(item.keys ?? []).map((key) => {
              const retryAt = key.retry_at ? Date.parse(key.retry_at) : 0
              const label =
                retryAt > Date.now()
                  ? t('Cooling down')
                  : (resultNames[key.result] ?? key.result)
              return (
                <Badge
                  key={key.slot}
                  variant={key.result === 'ok' ? 'secondary' : 'outline'}
                  title={
                    key.checked_at
                      ? `${t('Last checked')}: ${time.format(Date.parse(key.checked_at))}`
                      : undefined
                  }
                >
                  {t('Key')} {formatNumber(key.slot, locale)}: {label}
                  {key.status_code ? ` (${key.status_code})` : ''}
                </Badge>
              )
            })}
          </div>
        ))}
      </div>
    </section>
  )
}
