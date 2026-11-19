import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { api } from '@/lib/api'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

import {
  SettingsForm,
  SettingsFormGrid,
} from '../components/settings-form-layout'
import { SettingsSection } from '../components/settings-section'

type AlipaySettings = {
  app_id: string
  private_key_configured: boolean
  public_key: string
  notify_url: string
  return_url: string
  revision: string
}

type AlipaySettingsFormValues = Omit<
  AlipaySettings,
  'private_key_configured' | 'revision'
> & {
  private_key: string
}

type AlipaySettingsResponse = {
  success: boolean
  data: AlipaySettings
  message?: string
}

const endpoint = '/api/option/alipay'
const queryKey = ['alipay-settings'] as const

function AlipaySettingsForm(props: {
  settings: AlipaySettings
  onReload: () => void
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const form = useForm<AlipaySettingsFormValues>({
    defaultValues: {
      app_id: props.settings.app_id,
      public_key: props.settings.public_key,
      private_key: '',
      notify_url: props.settings.notify_url,
      return_url: props.settings.return_url,
    },
  })
  const textFields = [
    { name: 'app_id', label: t('Alipay App ID') },
    { name: 'notify_url', label: t('Alipay notify URL') },
    { name: 'return_url', label: t('Alipay return URL') },
  ] as const

  async function save(values: AlipaySettingsFormValues) {
    try {
      const response = await api.put<AlipaySettingsResponse>(endpoint, {
        ...values,
        revision: props.settings.revision,
      })
      const saved = requireServerSuccess(response.data).data
      form.reset({
        app_id: saved.app_id,
        public_key: saved.public_key,
        private_key: '',
        notify_url: saved.notify_url,
        return_url: saved.return_url,
      })
      client.setQueryData(queryKey, saved)
      toast.success(t('Settings updated successfully'))
    } catch (error) {
      handleServerError(error)
    }
  }

  return (
    <Form {...form}>
      <SettingsForm
        onSubmit={form.handleSubmit(save)}
        data-no-autosubmit='true'
      >
        <SettingsFormGrid>
          {textFields.map((item) => (
            <FormField
              key={item.name}
              control={form.control}
              name={item.name}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{item.label}</FormLabel>
                  <FormControl>
                    <Input {...field} autoComplete='off' />
                  </FormControl>
                </FormItem>
              )}
            />
          ))}
          <FormField
            control={form.control}
            name='public_key'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Alipay public key')}</FormLabel>
                <FormControl>
                  <Textarea
                    {...field}
                    autoComplete='off'
                    className='min-h-24 font-mono'
                  />
                </FormControl>
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='private_key'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('API Private Key')}</FormLabel>
                <FormControl>
                  <Textarea
                    {...field}
                    autoComplete='new-password'
                    spellCheck={false}
                    className='min-h-24 font-mono'
                  />
                </FormControl>
                <FormDescription>
                  {props.settings.private_key_configured
                    ? t('Leave blank to keep the existing key')
                    : t('Not configured')}
                </FormDescription>
              </FormItem>
            )}
          />
        </SettingsFormGrid>
        <div className='flex gap-2'>
          <Button
            type='submit'
            disabled={!form.formState.isDirty || form.formState.isSubmitting}
          >
            {t('Save Changes')}
          </Button>
          <Button
            type='button'
            variant='outline'
            disabled={form.formState.isSubmitting}
            onClick={props.onReload}
          >
            {t('Refresh')}
          </Button>
        </div>
      </SettingsForm>
    </Form>
  )
}

export function AlipaySettingsSection() {
  const { t } = useTranslation()
  const settings = useQuery({
    queryKey,
    refetchOnWindowFocus: false,
    queryFn: async () => {
      const response = await api.get<AlipaySettingsResponse>(endpoint)
      return requireServerSuccess(response.data).data
    },
  })

  return (
    <SettingsSection title={t('Alipay')}>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Alipay face-to-face is for wallet top-ups only. Enable Alipay in the payment methods to offer it.'
        )}
      </p>
      {settings.isPending && <LoadingState />}
      {settings.isError && !settings.data && (
        <ErrorState onRetry={() => void settings.refetch()} />
      )}
      {settings.data && (
        <AlipaySettingsForm
          key={settings.data.revision}
          settings={settings.data}
          onReload={() => void settings.refetch()}
        />
      )}
    </SettingsSection>
  )
}
