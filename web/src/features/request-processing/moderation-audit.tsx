import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { StaticDataTable } from '@/components/data-table'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { LoadingState } from '@/components/loading-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  SecureVerificationDialog,
  useSecureVerification,
} from '@/features/auth/secure-verification'
import { toIntlLocale } from '@/i18n/languages'
import { handleServerError } from '@/lib/handle-server-error'
import { getServerErrorMessage } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import { ModerationAuditDetails } from './components/moderation-audit-details'
import { ModerationAuditFilterBar } from './components/moderation-audit-filters'
import { ModerationAuditHealth } from './components/moderation-audit-health'
import {
  getModerationStatus,
  listModerationAudits,
  releaseModerationSuspension,
  type ModerationAuditFilters,
  type ModerationAuditRow,
} from './moderation-audit-api'
import { useModerationDecisionLabels } from './moderation-audit-labels'

export function ModerationAudit() {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const time = new Intl.DateTimeFormat(locale, {
    dateStyle: 'medium',
    timeStyle: 'short',
  })
  const decisions = useModerationDecisionLabels()
  const queryClient = useQueryClient()
  const actorId = useAuthStore((state) => state.auth.user?.id)
  const [draft, setDraft] = useState<ModerationAuditFilters>({})
  const [filters, setFilters] = useState<ModerationAuditFilters>({})
  const [cursors, setCursors] = useState([0])
  const [details, setDetails] = useState<ModerationAuditRow | null>(null)
  const verification = useSecureVerification()
  const scopeKey = ['content-moderation-audits', actorId]
  const audits = useQuery({
    queryKey: [...scopeKey, filters, cursors.at(-1)],
    queryFn: ({ signal }) =>
      listModerationAudits(
        { ...filters, before_id: cursors.at(-1) || undefined },
        signal
      ),
    gcTime: 0,
    meta: { errorToast: false },
  })
  const status = useQuery({
    queryKey: ['content-moderation-status', actorId],
    queryFn: ({ signal }) => getModerationStatus(signal),
    gcTime: 0,
    meta: { errorToast: false },
  })
  const release = useMutation({
    mutationFn: (request: { id: number; proof: string }) =>
      releaseModerationSuspension(request.id, request.proof),
    onSuccess: async () => {
      toast.success(t('User enabled'))
      await queryClient.invalidateQueries({ queryKey: scopeKey })
    },
  })
  const restore = async (item: ModerationAuditRow) => {
    try {
      const proof = await verification.requestVerification({
        scope: 'admin.user.manage',
        context: { user_id: item.user_id, action: 'enable' },
        title: t('Verify to enable user'),
        description: t(
          'Confirm your identity to release this moderation suspension.'
        ),
      })
      if (proof) release.mutate({ id: item.id, proof: proof.proof_token })
    } catch (error) {
      handleServerError(error)
    }
  }
  const more = audits.data?.next_before_id ?? 0

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Content audit records')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          variant='outline'
          nativeButton={false}
          render={<Link to='/request-processing' />}
        >
          {t('Request processing')}
        </Button>
        <Button
          variant='outline'
          onClick={() => {
            void audits.refetch()
            void status.refetch()
          }}
        >
          {t('Refresh')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='space-y-4'>
          {!status.isError && status.data && (
            <ModerationAuditHealth status={status.data} />
          )}
          <form
            className='space-y-3'
            onSubmit={(event) => {
              event.preventDefault()
              setDetails(null)
              setCursors([0])
              setFilters(draft)
            }}
          >
            <ModerationAuditFilterBar value={draft} onChange={setDraft} />
            <Button type='submit'>{t('Search')}</Button>
          </form>
          {audits.isPending && <LoadingState />}
          {audits.isError && (
            <ErrorState
              description={getServerErrorMessage(audits.error)}
              onRetry={() => void audits.refetch()}
            />
          )}
          {audits.isSuccess && audits.data.items.length === 0 && (
            <EmptyState title={t('No content audit records')} />
          )}
          {audits.isSuccess && audits.data.items.length > 0 && (
            <StaticDataTable
              tableProps={{ 'aria-label': t('Content audit records') }}
              data={audits.data.items}
              getRowKey={(item) => item.id}
              columns={[
                {
                  id: 'time',
                  header: t('Time'),
                  cell: (item) => time.format(item.created_at * 1000),
                },
                {
                  id: 'user',
                  header: t('User ID'),
                  cell: (item) => String(item.user_id),
                },
                {
                  id: 'instance',
                  header: t('Instance'),
                  cell: (item) => item.instance_name,
                },
                {
                  id: 'model',
                  header: t('Model'),
                  cell: (item) => item.model || '—',
                },
                {
                  id: 'stage',
                  header: t('Stage'),
                  cell: (item) =>
                    item.stage === 'reply' ? t('Response') : t('Request'),
                },
                {
                  id: 'decision',
                  header: t('Audit result'),
                  cell: (item) => (
                    <Badge variant='outline'>
                      {decisions[item.decision] ?? item.decision}
                    </Badge>
                  ),
                },
                {
                  id: 'actions',
                  header: t('Actions'),
                  cell: (item) => (
                    <div className='flex gap-1'>
                      <Button
                        variant='ghost'
                        size='sm'
                        onClick={() => setDetails(item)}
                      >
                        {t('Details')}
                      </Button>
                      {item.auto_disabled && !item.released_at && (
                        <Button
                          variant='outline'
                          size='sm'
                          disabled={verification.isActive || release.isPending}
                          onClick={() => void restore(item)}
                        >
                          {t('Release suspension')}
                        </Button>
                      )}
                    </div>
                  ),
                },
              ]}
            />
          )}
          <div className='flex justify-end gap-2'>
            <Button
              variant='outline'
              disabled={cursors.length === 1 || audits.isFetching}
              onClick={() => setCursors((current) => current.slice(0, -1))}
            >
              {t('Previous')}
            </Button>
            <Button
              variant='outline'
              disabled={!more || audits.isFetching}
              onClick={() => setCursors((current) => [...current, more])}
            >
              {t('Next')}
            </Button>
          </div>
        </div>
        {details && audits.isSuccess && (
          <ModerationAuditDetails
            item={details}
            onClose={() => setDetails(null)}
          />
        )}
        <SecureVerificationDialog {...verification.dialogProps} />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
