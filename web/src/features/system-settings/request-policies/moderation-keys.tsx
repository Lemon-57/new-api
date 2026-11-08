import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import { getPolicyConfig, type ModerationKeyStatus } from './api'
import { useSavePolicy } from './use-save-policy'

export function ModerationKeys() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const query = useQuery({ queryKey: ['moderation-key-status'], queryFn: getPolicyConfig })
  const save = useSavePolicy()
  const [deleting, setDeleting] = useState<ModerationKeyStatus | null>(null)
  const keys = query.data?.moderation_keys ?? []
  return (
    <div className='space-y-3'>
      <div className='flex items-center justify-between gap-2'>
        <span>{t('Configured keys')}: {formatNumber(keys.length, locale)}</span>
        <Button type='button' variant='outline' size='sm' disabled={query.isFetching} onClick={() => void query.refetch()}>{t('Refresh')}</Button>
      </div>
      {query.isError && <p role='alert'>{t('Failed to load key status')}</p>}
      {keys.map((key) => (
        <div key={key.hash} className='flex flex-wrap items-center justify-between gap-3 rounded-md border p-3 text-sm'>
          <div className='space-y-1'>
            <p className='font-mono'>{key.masked}</p>
            <p>{new Date(key.frozen_until).getTime() > Date.now() ? t('Temporarily frozen') : t('Available')} · {t('Calls')}: {formatNumber(key.calls, locale)} · {t('Successful')}: {formatNumber(key.successes, locale)} · {t('Failed')}: {formatNumber(key.errors, locale)} · {t('Active')}: {formatNumber(key.active, locale)}</p>
            <p>{t('Last HTTP status')}: {key.last_http_status || '—'} · {t('Last latency (ms)')}: {formatNumber(key.last_latency_ms, locale)} · {t('Average latency (ms)')}: {formatNumber(key.calls ? Math.round(key.latency_ms / key.calls) : 0, locale)}</p>
            {new Date(key.frozen_until).getTime() > Date.now() && <p>{t('Freeze remaining (seconds)')}: {formatNumber(Math.ceil((new Date(key.frozen_until).getTime() - Date.now()) / 1000), locale)}</p>}
          </div>
          <Button type='button' variant='destructive' size='sm' onClick={() => setDeleting(key)} disabled={save.isPending}>{t('Delete')}</Button>
        </div>
      ))}
      <p className='text-muted-foreground text-sm'>{t('Keys rotate across requests and retries. Frozen keys are skipped. Statistics reset when the service restarts.')}</p>
      <ConfirmDialog open={deleting !== null} onOpenChange={(open) => { if (!open) setDeleting(null) }} title={t('Delete moderation key?')} desc={t('New requests will stop using this key. Existing requests continue. Deleting the last key follows the unavailable-service policy.')} destructive isLoading={save.isPending} handleConfirm={() => {
        if (!deleting) return
        save.mutate({ ContentModerationDeleteKey: deleting.hash }, { onSuccess: () => setDeleting(null) })
      }} />
    </div>
  )
}
