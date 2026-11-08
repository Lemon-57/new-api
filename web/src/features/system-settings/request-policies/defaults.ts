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
import type { ChannelAffinitySettings } from '../general/channel-affinity/types'
import type { SecuritySettings } from '../types'

export type RetrySettings = {
  RetryTimes: number
  AutomaticRetryStatusCodes: string
}
export type HealthSettings = {
  ChannelDisableThreshold: string
  AutomaticDisableChannelEnabled: boolean
  AutomaticEnableChannelEnabled: boolean
  AutomaticDisableKeywords: string
  AutomaticDisableStatusCodes: string
  'monitor_setting.auto_test_channel_enabled': boolean
  'monitor_setting.auto_test_channel_minutes': number
  'monitor_setting.channel_test_concurrency': number
  'monitor_setting.channel_test_mode':
    | 'scheduled_all'
    | 'auto_ban_only'
    | 'passive_recovery'
}
export type FilteringSettings = Pick<
  SecuritySettings,
  'CheckSensitiveEnabled' | 'CheckSensitiveOnPromptEnabled' | 'SensitiveWords'
> & {
  ContentModerationEnabled: boolean
  ContentModerationEngine: string
  ContentModerationMode: string
  ContentModerationBaseURL: string
  ContentModerationModel: string
  ContentModerationAPIKey: string
  ContentModerationKeyConfigured: boolean
  ContentModerationTimeoutMS: number
  ContentModerationRetryCount: number
  ContentModerationSampleRate: number
  ContentModerationFailOpen: boolean
  ContentModerationThresholds: string
}
export type RequestPolicySettings = RetrySettings &
  HealthSettings &
  FilteringSettings &
  Pick<ChannelAffinitySettings, keyof ChannelAffinitySettings>

export const defaultRequestPolicySettings: RequestPolicySettings = {
  RetryTimes: 0,
  AutomaticRetryStatusCodes:
    '100-199,300-399,401-407,409-499,500-503,505-523,525-599',
  ChannelDisableThreshold: '',
  AutomaticDisableChannelEnabled: false,
  AutomaticEnableChannelEnabled: false,
  AutomaticDisableKeywords: '',
  AutomaticDisableStatusCodes: '401',
  'monitor_setting.auto_test_channel_enabled': false,
  'monitor_setting.auto_test_channel_minutes': 10,
  'monitor_setting.channel_test_concurrency': 1,
  'monitor_setting.channel_test_mode': 'scheduled_all',
  'channel_affinity_setting.enabled': false,
  'channel_affinity_setting.session_mode': '',
  'channel_affinity_setting.switch_on_success': true,
  'channel_affinity_setting.keep_on_channel_disabled': false,
  'channel_affinity_setting.max_entries': 100000,
  'channel_affinity_setting.default_ttl_seconds': 3600,
  'channel_affinity_setting.rules': '[]',
  CheckSensitiveEnabled: false,
  CheckSensitiveOnPromptEnabled: false,
  SensitiveWords: '',
  ContentModerationEnabled: false,
  ContentModerationEngine: 'typesafe',
  ContentModerationMode: 'pre_block',
  ContentModerationBaseURL: 'https://jev.bocha.cn',
  ContentModerationModel: 'bocha-jev-v1',
  ContentModerationAPIKey: '',
  ContentModerationKeyConfigured: false,
  ContentModerationTimeoutMS: 3000,
  ContentModerationRetryCount: 2,
  ContentModerationSampleRate: 100,
  ContentModerationFailOpen: false,
  ContentModerationThresholds:
    '{"harassment":0.98,"harassment/threatening":0.9,"hate":0.65,"hate/threatening":0.65,"illicit":0.95,"illicit/violent":0.95,"self-harm":0.65,"self-harm/instructions":0.65,"self-harm/intent":0.85,"sexual":0.65,"sexual/minors":0.65,"violence":0.95,"violence/graphic":0.95}',
}
