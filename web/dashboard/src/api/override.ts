import http, { get } from './http'
import type { ConditionalOverride, CreateOverrideRequest } from '@/types/models'

/** 获取所有干预记录 */
export const fetchOverrides = () => get<ConditionalOverride[]>('/overrides')

/** 创建条件干预 */
export const createOverride = async (req: CreateOverrideRequest): Promise<ConditionalOverride> => {
  const res = await http.post('/overrides', req)
  return res.data.data
}

/** 取消条件干预 */
export const cancelOverride = async (id: string): Promise<void> => {
  await http.delete(`/overrides/${id}`)
}

/** 立即强制平仓 */
export const forceClosePosition = async (symbol: string, note = '手动平仓'): Promise<void> => {
  await http.post(`/positions/${symbol}/force-close`, { note })
}

/** 立即暂停策略 */
export const pauseStrategy = async (hours = 24, reason = '手动暂停'): Promise<void> => {
  await http.post('/strategy/pause', { hours, reason })
}

/** 恢复策略 */
export const resumeStrategy = async (): Promise<void> => {
  await http.post('/strategy/resume')
}
