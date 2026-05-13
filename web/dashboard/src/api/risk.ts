import http, { get } from './http'
import type { RiskStatus, RiskLimits } from '@/types/models'

export const fetchRiskStatus = () => get<RiskStatus>('/risk/status')

export const fetchRiskLimits = () => get<RiskLimits>('/risk/limits')

export const setRiskLimits = async (limits: Partial<RiskLimits>): Promise<RiskLimits> => {
  const res = await http.post('/risk/limits', limits)
  return res.data.data
}
