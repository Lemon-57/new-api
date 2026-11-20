import type { ApiResponse } from '@/features/profile/types'
import { api } from '@/lib/api'
import {
  createServerError,
  requireServerSuccess,
} from '@/lib/server-error-message'

export type ModerationAuditRow = {
  id: number
  request_id: string
  user_id: number
  instance_id: string
  instance_name: string
  model: string
  engine: string
  engine_model: string
  stage: string
  mode: string
  decision: string
  category?: string
  scores?: string
  thresholds?: string
  input_excerpt?: string
  failure_class?: string
  hit_email?: string
  ban_email?: string
  auto_disabled: boolean
  released_at?: number
  created_at: number
  expires_at: number
}

export type ModerationAuditFilters = {
  request_id?: string
  instance_id?: string
  user_id?: number
  decision?: string
  stage?: string
  start?: number
  end?: number
  before_id?: number
}

export type ModerationStatus = {
  node: string
  version: string
  queues: Record<string, { active: number; pending: number; dropped: number }>
  instances: {
    instance_id: string
    name: string
    engine: string
    keys:
      | {
          slot: number
          result: string
          status_code?: number
          checked_at?: string
          retry_at?: string
        }[]
      | null
  }[]
}

type ModerationAuditPage = {
  items: ModerationAuditRow[]
  next_before_id: number
}

export async function listModerationAudits(
  filters: ModerationAuditFilters,
  signal?: AbortSignal
): Promise<ModerationAuditPage> {
  const response = await api.get<ApiResponse<ModerationAuditPage>>(
    '/api/request_plugin/moderation/audits',
    { params: filters, signal }
  )
  const data = requireServerSuccess(response.data).data
  if (!data) throw createServerError(response.data, 'Invalid response')
  return data
}

export async function getModerationStatus(
  signal?: AbortSignal
): Promise<ModerationStatus> {
  const response = await api.get<ApiResponse<ModerationStatus>>(
    '/api/request_plugin/moderation/status',
    { signal }
  )
  const data = requireServerSuccess(response.data).data
  if (!data) throw createServerError(response.data, 'Invalid response')
  return data
}

export async function releaseModerationSuspension(
  id: number,
  proof: string
): Promise<void> {
  const response = await api.post<ApiResponse<null>>(
    `/api/request_plugin/moderation/audits/${id}/release`,
    null,
    { headers: { 'X-Security-Proof': proof } }
  )
  requireServerSuccess(response.data)
}
