import { useTranslation } from 'react-i18next'

import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import type { ModerationAuditFilters } from '../moderation-audit-api'
import { useModerationDecisionLabels } from '../moderation-audit-labels'

export function ModerationAuditFilterBar(props: {
  value: ModerationAuditFilters
  onChange: (value: ModerationAuditFilters) => void
}) {
  const { t } = useTranslation()
  const decisions = useModerationDecisionLabels()
  const selectOptions = [
    { key: 'decision' as const, title: t('Audit result'), items: decisions },
    {
      key: 'stage' as const,
      title: t('Stage'),
      items: { ingress: t('Request'), reply: t('Response') },
    },
  ]
  const textFields = [
    { key: 'request_id' as const, title: t('Request ID') },
    { key: 'instance_id' as const, title: t('Instance ID') },
  ]

  return (
    <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-4'>
      {textFields.map((item) => (
        <Field key={item.key}>
          <FieldLabel htmlFor={`moderation-${item.key}`}>
            {item.title}
          </FieldLabel>
          <Input
            id={`moderation-${item.key}`}
            maxLength={128}
            value={props.value[item.key] ?? ''}
            onChange={(event) =>
              props.onChange({ ...props.value, [item.key]: event.target.value })
            }
          />
        </Field>
      ))}
      <Field>
        <FieldLabel htmlFor='moderation-user'>{t('User ID')}</FieldLabel>
        <Input
          id='moderation-user'
          type='number'
          min={1}
          step={1}
          value={props.value.user_id ?? ''}
          onChange={(event) =>
            props.onChange({
              ...props.value,
              user_id: event.target.valueAsNumber || undefined,
            })
          }
        />
      </Field>
      {selectOptions.map((option) => (
        <Field key={option.key}>
          <FieldLabel htmlFor={`moderation-${option.key}`}>
            {option.title}
          </FieldLabel>
          <Select
            value={props.value[option.key] ?? 'all'}
            items={[
              { value: 'all', label: t('All') },
              ...Object.entries(option.items).map(([value, label]) => ({
                value,
                label,
              })),
            ]}
            onValueChange={(value) =>
              props.onChange({
                ...props.value,
                [option.key]:
                  value === 'all' ? undefined : (value ?? undefined),
              })
            }
          >
            <SelectTrigger id={`moderation-${option.key}`} className='w-full'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='all'>{t('All')}</SelectItem>
              {Object.entries(option.items).map(([value, label]) => (
                <SelectItem key={value} value={value}>
                  {label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
      ))}
      {(
        [
          { key: 'start', title: t('Start Time') },
          { key: 'end', title: t('End Time') },
        ] as const
      ).map((item) => (
        <Field key={item.key}>
          <FieldLabel htmlFor={`moderation-${item.key}`}>
            {item.title}
          </FieldLabel>
          <Input
            id={`moderation-${item.key}`}
            type='datetime-local'
            onChange={(event) => {
              const parsed = Date.parse(event.target.value)
              props.onChange({
                ...props.value,
                [item.key]: Number.isFinite(parsed)
                  ? Math.floor(parsed / 1000)
                  : undefined,
              })
            }}
          />
        </Field>
      ))}
    </div>
  )
}
