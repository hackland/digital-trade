import { get } from './http'
import type { RiskStatus } from '@/types/models'

export const fetchRiskStatus = () => get<RiskStatus>('/risk/status')
