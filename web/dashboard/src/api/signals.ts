import { getPaginated } from './http'
import type { SignalRecord } from '@/types/models'
import type { PaginatedResponse } from '@/types/api'

export const fetchSignals = (params?: Record<string, any>): Promise<PaginatedResponse<SignalRecord[]>> =>
  getPaginated<SignalRecord[]>('/signals', params)
