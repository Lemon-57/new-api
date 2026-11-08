import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

export type PolicyConfig = {
  options: Record<string, string>
  moderation_keys?: ModerationKeyStatus[]
}
export type ModerationKeyStatus = {
  hash: string
  masked: string
  frozen_until: string
  last_checked_at: string
  last_http_status: number
  last_latency_ms: number
  last_error: string
  calls: number
  successes: number
  errors: number
  active: number
  latency_ms: number
}
export type PolicyDecision = { action: string; reason: string; source: string }
export type PolicyEvent = {
  moderation?: {
    decision: string
    engine: string
    model: string
    latency_ms: number
    scores?: Record<string, number>
    thresholds?: Record<string, number>
  }
  attempt: number
  channel_id?: number
  group?: string
  rule?: string
  status?: number
  error_code?: string
  error_source?: string
  elapsed_ms: number
  decision: PolicyDecision
  health?: string
}
type Response<T> = { success: boolean; message?: string; data: T }
export async function getPolicyConfig() {
  const response = await api.get<Response<PolicyConfig>>(
    '/api/option/request_policy'
  )
  return requireServerSuccess(response.data).data
}
export async function savePolicyConfig(options: Record<string, string>) {
  const response = await api.patch<Response<PolicyConfig>>(
    '/api/option/request_policy',
    { options }
  )
  return requireServerSuccess(response.data).data
}
