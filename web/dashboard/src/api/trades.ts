import { getPaginated } from './http'
import type { TradeRecord } from '@/types/models'
import type { PaginatedResponse } from '@/types/api'

export const fetchTrades = (params?: Record<string, any>): Promise<PaginatedResponse<TradeRecord[]>> =>
  getPaginated<TradeRecord[]>('/trades', params)
