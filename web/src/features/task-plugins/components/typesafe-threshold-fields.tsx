import { RotateCcw } from 'lucide-react'
import type { ComponentProps } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { FieldDescription, FieldLegend, FieldSet } from '@/components/ui/field'
import { resolveLocalizedText } from '@/lib/localized-text'
import { cn } from '@/lib/utils'

import { newRequestPluginConfigRow } from '../lib/request-plugins'
import { RequestPluginConfigField } from './request-plugin-config-field'
import { RequestPluginConfigRows } from './request-plugin-config-rows'

type ThresholdDefault = { category: string; threshold: number }

export function TypeSafeThresholdFields(
  props: ComponentProps<typeof RequestPluginConfigRows>
) {
  const { t, i18n } = useTranslation()
  const fields = props.field.items?.properties
  const categoryField = fields?.category
  const thresholdField = fields?.threshold
  const example = props.field.examples?.[0]
  const defaults = Array.isArray(example)
    ? example.filter(
        (entry: unknown): entry is ThresholdDefault =>
          entry !== null &&
          typeof entry === 'object' &&
          'category' in entry &&
          typeof entry.category === 'string' &&
          'threshold' in entry &&
          typeof entry.threshold === 'number'
      )
    : []
  if (!defaults.length || !categoryField || thresholdField?.type !== 'number') {
    return <RequestPluginConfigRows {...props} />
  }

  const title = resolveLocalizedText(props.field.title, i18n.language)
  const titleId = `${props.id}-title`
  const description = resolveLocalizedText(
    props.field.description,
    i18n.language
  )
  const resetLabel = t('Reset to default')
  const savedByCategory = new Map(
    props.rows.map((row) => [row.values.category, row])
  )

  return (
    <FieldSet
      aria-labelledby={titleId}
      className={cn('gap-3', props.className)}
    >
      <div className='space-y-0.5'>
        <FieldLegend id={titleId} variant='label' className='mb-0'>
          {title || props.name}
        </FieldLegend>
        {description && <FieldDescription>{description}</FieldDescription>}
      </div>
      <div className='grid gap-2'>
        {defaults.map(({ category, threshold }) => {
          const index = categoryField.enum?.indexOf(category) ?? -1
          const label =
            resolveLocalizedText(
              categoryField.enumTitles?.[index],
              i18n.language
            ) || category
          const row = savedByCategory.get(category)
          return (
            <div
              key={category}
              className='grid grid-cols-[minmax(0,1fr)_7rem_2rem] items-start gap-2'
            >
              <span
                aria-hidden='true'
                className='flex min-h-8 items-center text-sm'
              >
                {label}
              </span>
              <RequestPluginConfigField
                id={`${props.id}-${category}`}
                name={category}
                field={{ ...thresholdField, title: label }}
                value={row?.values.threshold ?? String(threshold)}
                disabled={props.disabled}
                error={
                  row && props.errorFor(`${props.name}.${row.key}.threshold`)
                }
                layout='cell'
                onChange={(value) => {
                  const next = row
                    ? { ...row, values: { ...row.values, threshold: value } }
                    : newRequestPluginConfigRow({ category, threshold: value })
                  props.onChange(
                    row
                      ? props.rows.map((item) =>
                          item.key === row.key ? next : item
                        )
                      : [...props.rows, next],
                    `${props.name}.${next.key}.threshold`
                  )
                }}
              />
              <Button
                type='button'
                variant='ghost'
                size='icon-sm'
                disabled={props.disabled || !row}
                aria-label={`${resetLabel}: ${label}`}
                title={resetLabel}
                onClick={() =>
                  props.onChange(props.rows.filter((item) => item !== row))
                }
              >
                <RotateCcw aria-hidden='true' />
              </Button>
            </div>
          )
        })}
      </div>
    </FieldSet>
  )
}
