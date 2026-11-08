/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { zodResolver } from '@hookform/resolvers/zod'
import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { handleServerError } from '@/lib/handle-server-error'

import {
  SettingsForm,
  SettingsControlGroup,
  SettingsControlChildren,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useSavePolicy } from './use-save-policy'
import { ModerationKeys } from './moderation-keys'

const sensitiveSchema = z.object({
  CheckSensitiveEnabled: z.boolean(),
  CheckSensitiveOnPromptEnabled: z.boolean(),
  SensitiveWords: z.string().optional(),
  ContentModerationEnabled: z.boolean(),
  ContentModerationEngine: z.string(),
  ContentModerationTimeoutMS: z.number().int().min(100).max(60000),
  ContentModerationRetryCount: z.number().int().min(0).max(5),
  ContentModerationSampleRate: z.number().int().min(0).max(100),
  ContentModerationMode: z.string(),
  ContentModerationBaseURL: z.string(),
  ContentModerationModel: z.string(),
  ContentModerationAPIKey: z.string().optional(),
  ContentModerationKeyConfigured: z.boolean(),
  ContentModerationFailOpen: z.boolean(),
  ContentModerationThresholds: z.string(),
})

type SensitiveFormValues = z.infer<typeof sensitiveSchema>

type RequestChecksSectionProps = {
  defaultValues: SensitiveFormValues
}

export function RequestChecksSection({
  defaultValues,
}: RequestChecksSectionProps) {
  const { t } = useTranslation()
  const updateOption = useSavePolicy()
  const [keyMode, setKeyMode] = useState('append')
  const form = useForm<SensitiveFormValues>({
    resolver: zodResolver(sensitiveSchema),
    defaultValues,
  })

  useEffect(() => {
    form.reset(defaultValues)
  }, [defaultValues, form])

  const onSubmit = async (values: SensitiveFormValues) => {
    const updates = Object.entries(values).filter(
      ([key, value]) =>
        key !== 'ContentModerationKeyConfigured' &&
        !(key === 'ContentModerationAPIKey' && !String(value ?? '').trim()) &&
        value !== defaultValues[key as keyof SensitiveFormValues]
    )

    try {
      if (updates.length > 0) {
        const options = Object.fromEntries(
            updates.map(([key, value]) => [key, String(value ?? '')])
          )
        if (options.ContentModerationAPIKey) options.ContentModerationKeyUpdateMode = keyMode
        await updateOption.mutateAsync(options)
        form.setValue('ContentModerationAPIKey', '')
      }
    } catch (error) {
      handleServerError(error)
    }
  }

  return (
    <SettingsSection title={t('Request checks')}>
      <p className='text-muted-foreground text-sm'>
        {t('Source: global settings. Changes take effect after saving.')}
      </p>
      <h3 className='text-sm font-medium'>{t('Request text filtering')}</h3>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Checks text extracted from supported requests against keywords, ignoring case. A match rejects the request before upstream processing and does not affect channel health. Images, audio and generated responses are not checked.'
        )}
      </p>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={form.formState.isSubmitting}
            saveLabel='Save request checks'
          />
          <Alert>
            <AlertDescription>
              {form.watch('CheckSensitiveEnabled') &&
              form.watch('CheckSensitiveOnPromptEnabled') &&
              form.watch('SensitiveWords')?.trim()
                ? t(
                    'Prompt text filtering is active with the current form values.'
                  )
                : t(
                    'Prompt text filtering needs both switches enabled and a non-empty keyword list.'
                  )}
            </AlertDescription>
          </Alert>
          <SettingsControlGroup>
            <FormField
              control={form.control}
              name='CheckSensitiveEnabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Enable filtering')}</FormLabel>
                    <FormDescription>
                      {t(
                        'Blocks messages when sensitive keywords are detected.'
                      )}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />

            <SettingsControlChildren>
              <FormField
                control={form.control}
                name='CheckSensitiveOnPromptEnabled'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>{t('Inspect user prompts')}</FormLabel>
                      <FormDescription>
                        {t(
                          'When enabled, prompts are scanned before reaching upstream models.'
                        )}
                      </FormDescription>
                    </SettingsSwitchContent>
                    <FormControl>
                      <Switch
                        disabled={!form.watch('CheckSensitiveEnabled')}
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </SettingsSwitchItem>
                )}
              />
            </SettingsControlChildren>
          </SettingsControlGroup>

          <FormField
            control={form.control}
            name='SensitiveWords'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Blocked keywords')}</FormLabel>
                <FormControl>
                  <Textarea
                    rows={12}
                    placeholder={t('Enter one keyword per line')}
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Each line represents one keyword. Leave blank to disable the list but keep the switch states.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <div className='border-border mt-6 space-y-4 rounded-lg border p-4'>
            <div>
              <h3 className='text-sm font-medium'>{t('AI content moderation')}</h3>
              <p className='text-muted-foreground text-sm'>
                {t(
                  'Runs a TypeSafe Jev check after local keyword filtering and before the upstream model. Blocked requests return HTTP 403.'
                )}
              </p>
            </div>
            <FormField
              control={form.control}
              name='ContentModerationEnabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Enable AI moderation')}</FormLabel>
                    <FormDescription>
                      {t('Use TypeSafe Jev to inspect supported request text.')}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch checked={field.value} onCheckedChange={field.onChange} />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
            <SettingsControlChildren>
              <FormField
                control={form.control}
                name='ContentModerationMode'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('AI moderation mode')}</FormLabel>
                    <Select value={field.value} onValueChange={(value) => value && field.onChange(value)} items={[{ value: 'pre_block', label: t('Pre-block') }, { value: 'observe', label: t('Observe only') }]}>
                      <FormControl><SelectTrigger className='w-full'><SelectValue /></SelectTrigger></FormControl>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectItem value='pre_block'>{t('Pre-block')}</SelectItem>
                        <SelectItem value='observe'>{t('Observe only')}</SelectItem>
                      </SelectContent>
                    </Select>
                  </FormItem>
                )}
              />
            </SettingsControlChildren>
            <div className='grid gap-4 md:grid-cols-2'>
              <FormField
                control={form.control}
                name='ContentModerationBaseURL'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Jev Base URL')}</FormLabel>
                    <FormControl><Input {...field} /></FormControl>
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='ContentModerationModel'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Jev model')}</FormLabel>
                    <FormControl><Input {...field} /></FormControl>
                  </FormItem>
                )}
              />
            </div>
            <FormField
              control={form.control}
              name='ContentModerationAPIKey'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Jev API keys')}</FormLabel>
                  <FormControl>
                    <Textarea rows={3} className='[-webkit-text-security:disc]' autoComplete='new-password' spellCheck={false} placeholder={form.watch('ContentModerationKeyConfigured') ? t('Configured; leave blank to keep it') : ''} {...field} />
                  </FormControl>
                  <FormDescription>
                    {t('Enter one key per line. Duplicates are removed. Blank keeps existing keys. Keys are encrypted and never returned.')}
                  </FormDescription>
                </FormItem>
              )}
            />
            <FormItem>
              <FormLabel htmlFor='moderation-key-mode'>{t('Key update mode')}</FormLabel>
              <Select value={keyMode} onValueChange={(value) => { if (value) setKeyMode(value) }}>
                <SelectTrigger id='moderation-key-mode'><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value='append'>{t('Append keys')}</SelectItem>
                  <SelectItem value='replace'>{t('Replace all keys')}</SelectItem>
                </SelectContent>
              </Select>
            </FormItem>
            <ModerationKeys />
            <FormField
              control={form.control}
              name='ContentModerationFailOpen'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Allow requests when Jev is unavailable')}</FormLabel>
                    <FormDescription>
                      {t('When disabled, an unavailable moderation service returns HTTP 503.')}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl><Switch checked={field.value} onCheckedChange={field.onChange} /></FormControl>
                </SettingsSwitchItem>
              )}
            />
            <FormField
              control={form.control}
              name='ContentModerationThresholds'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Jev category thresholds')}</FormLabel>
                  <FormControl><Textarea rows={4} {...field} /></FormControl>
                  <FormDescription>{t('JSON map of category names to scores from 0 to 1.')}</FormDescription>
                </FormItem>
              )}
            />
            <div className='grid gap-4 md:grid-cols-3'>
              {([
                { name: 'ContentModerationTimeoutMS', label: t('Moderation timeout (ms)'), min: 100, max: 60000 },
                { name: 'ContentModerationRetryCount', label: t('Moderation retries'), min: 0, max: 5 },
                { name: 'ContentModerationSampleRate', label: t('Moderation sample rate (%)'), min: 0, max: 100 },
              ] as const).map((item) => (
                <FormField key={item.name} control={form.control} name={item.name} render={({ field }) => (
                  <FormItem>
                    <FormLabel>{item.label}</FormLabel>
                    <FormControl><Input {...field} type='number' min={item.min} max={item.max} step={1} onChange={(event) => field.onChange(event.target.valueAsNumber)} /></FormControl>
                    <FormMessage />
                  </FormItem>
                )} />
              ))}
            </div>
          </div>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
