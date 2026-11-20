import { useTranslation } from 'react-i18next'

export function useModerationDecisionLabels(): Record<string, string> {
  const { t } = useTranslation()
  return {
    blocked: t('Blocked'),
    observed_hit: t('Observed hit'),
    hash_block: t('Risk hash match'),
    allowed: t('Allowed'),
    error: t('Error'),
    skipped: t('Skipped'),
  }
}
