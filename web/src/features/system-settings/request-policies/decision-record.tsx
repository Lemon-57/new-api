import { useTranslation } from 'react-i18next'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import type { PolicyEvent } from './api'
import { policyLabel } from './policy-label'

export function PolicyDecisionRecord(props: {
  events: PolicyEvent[] | null
  channelNames?: Record<number, string>
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  // Moderation scores need more than the ordinary two-decimal display precision
  // so administrators can compare scores close to a configured threshold.
  const scoreFormat = new Intl.NumberFormat(locale, { maximumFractionDigits: 6 })
  return (
    <ol aria-label={t('Request decision flow')} className='space-y-0 text-sm'>
      {(props.events ?? []).map((event, index) => (
        <li
          key={`${event.attempt}:${event.channel_id ?? 0}:${event.decision.action}:${event.decision.reason}:${event.rule ?? ''}`}
          className='border-border relative ml-3 min-w-0 border-l pb-5 pl-6 last:border-transparent last:pb-1'
        >
          <span
            aria-hidden='true'
            className='border-border bg-background absolute -left-3 flex size-6 items-center justify-center rounded-full border text-xs'
          >
            {index + 1}
          </span>
          <p
            className={
              event.decision.action === 'stop'
                ? 'font-medium text-amber-700 dark:text-amber-400'
                : 'font-medium'
            }
          >
            {policyLabel(t, event.decision.reason)}
            {event.channel_id
              ? ` · ${props.channelNames?.[event.channel_id] ?? `#${event.channel_id}`}`
              : ''}
            {event.status && event.decision.action === 'failure'
              ? ` · HTTP ${event.status}`
              : ''}
          </p>
          <p className='text-muted-foreground text-xs break-words'>
            {event.attempt > 0 ? `${t('Attempt')} ${event.attempt} · ` : ''}
            {event.elapsed_ms} ms
            {event.group ? ` · ${event.group}` : ''}
            {event.rule ? ` · ${event.rule}` : ''}
            {event.status ? ` · HTTP ${event.status}` : ''}
          </p>
          <p className='text-muted-foreground text-xs'>
            {t('Source')}: {policyLabel(t, event.decision.source)}
            {event.health ? ` · ${policyLabel(t, event.health)}` : ''}
          </p>
          {event.moderation && (
            <div className='mt-2 space-y-1'>
              <p>{event.moderation.engine} · {event.moderation.model} · {formatNumber(event.moderation.latency_ms, locale)} ms</p>
              <dl aria-label={t('Moderation scores and thresholds')} className='grid grid-cols-1 gap-1 text-xs sm:grid-cols-2'>
                {Object.entries(event.moderation.scores ?? {}).map(([category, score]) => (
                  <div key={category} className='flex flex-wrap gap-2'>
                    <dt>{category}</dt>
                    <dd>
                      {scoreFormat.format(score)}
                      {event.moderation?.thresholds?.[category] !== undefined && (
                        <> · {t('Threshold')}: {scoreFormat.format(event.moderation.thresholds[category])}</>
                      )}
                    </dd>
                  </div>
                ))}
              </dl>
            </div>
          )}
        </li>
      ))}
    </ol>
  )
}
