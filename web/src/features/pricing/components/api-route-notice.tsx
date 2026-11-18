import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import type { ApiInfoItem } from '@/features/dashboard/types'

export function ApiRouteNotice(props: { routes: ApiInfoItem[] }) {
  const { t } = useTranslation()
  if (props.routes.length < 2) return null

  return (
    <section
      aria-label={t('API Addresses')}
      className='bg-card mb-6 rounded-xl border p-4'
    >
      <h2 className='font-medium'>{t('API Addresses')}</h2>
      <div className='mt-3 grid gap-3 sm:grid-cols-2'>
        {props.routes.map((route) => (
          <div key={route.url} className='bg-muted/40 rounded-lg p-3 text-sm'>
            <div className='flex items-center justify-between gap-2'>
              <span className='font-medium'>{route.route}</span>
              <CopyButton value={route.url} aria-label={t('Copy URL')} />
            </div>
            <code className='mt-1 block break-all'>{route.url}</code>
            <p className='text-muted-foreground mt-1'>{route.description}</p>
          </div>
        ))}
      </div>
    </section>
  )
}
