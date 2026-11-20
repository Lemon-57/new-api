import { useTranslation } from 'react-i18next'

import { StaticDataTable } from '@/components/data-table'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { toIntlLocale } from '@/i18n/languages'

import type { ModerationAuditRow } from '../moderation-audit-api'

function scoreValues(source?: string): Record<string, number> {
  if (!source) return {}
  try {
    return JSON.parse(source) as Record<string, number>
  } catch {
    return {}
  }
}

export function ModerationAuditDetails(props: {
  item: ModerationAuditRow
  onClose: () => void
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const scores = scoreValues(props.item.scores)
  const thresholds = scoreValues(props.item.thresholds)
  const categories = [
    ...new Set([...Object.keys(scores), ...Object.keys(thresholds)]),
  ]
  const labels: Record<string, string> = {
    queued: t('Queued'),
    accepted: t('Accepted by mail service'),
    failed: t('Failed'),
    not_sent: t('Not sent'),
    enforce: t('Enforce synchronously'),
    observe: t('Observe in the background'),
    only_record: t('Log only'),
  }
  const time = new Intl.DateTimeFormat(locale, {
    dateStyle: 'medium',
    timeStyle: 'short',
  })
  const scoreNumber = new Intl.NumberFormat(locale, {
    maximumFractionDigits: 6,
  })
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
    >
      <DialogContent className='max-h-[85vh] overflow-y-auto sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{t('Content audit details')}</DialogTitle>
          <DialogDescription>{props.item.request_id}</DialogDescription>
        </DialogHeader>
        <dl className='grid gap-3 text-sm sm:grid-cols-2'>
          {[
            { label: t('Instance ID'), value: props.item.instance_id },
            {
              label: t('Audit mode'),
              value: labels[props.item.mode] ?? props.item.mode,
            },
            {
              label: t('Engine'),
              value: [props.item.engine, props.item.engine_model]
                .filter(Boolean)
                .join(' / '),
            },
            { label: t('Category'), value: props.item.category },
            {
              label: t('Hit email'),
              value: labels[props.item.hit_email ?? ''],
            },
            {
              label: t('Suspension email'),
              value: labels[props.item.ban_email ?? ''],
            },
            {
              label: t('Expires At'),
              value: time.format(props.item.expires_at * 1000),
            },
            { label: t('Error'), value: props.item.failure_class },
          ].map((item) => (
            <div key={item.label}>
              <dt className='text-muted-foreground'>{item.label}</dt>
              <dd className='break-all'>{item.value || '—'}</dd>
            </div>
          ))}
        </dl>
        {categories.length > 0 && (
          <StaticDataTable
            tableProps={{ 'aria-label': t('Category scores and thresholds') }}
            data={categories}
            getRowKey={(category) => category}
            columns={[
              {
                id: 'category',
                header: t('Category'),
                cell: (category) => category,
              },
              {
                id: 'score',
                header: t('Score'),
                cell: (category) =>
                  scores[category] === undefined
                    ? '—'
                    : scoreNumber.format(scores[category]),
              },
              {
                id: 'threshold',
                header: t('Threshold'),
                cell: (category) =>
                  thresholds[category] === undefined
                    ? '—'
                    : scoreNumber.format(thresholds[category]),
              },
            ]}
          />
        )}
        <div className='space-y-2'>
          <h3 className='text-sm font-medium'>{t('Redacted input excerpt')}</h3>
          <p className='bg-muted rounded-md p-3 text-sm break-words whitespace-pre-wrap'>
            {props.item.input_excerpt || t('No excerpt was stored.')}
          </p>
        </div>
      </DialogContent>
    </Dialog>
  )
}
