import { get } from './http'
import type { Kline } from '@/types/models'

export const fetchKlines = (params: { symbol: string; interval: string; limit?: number; start?: string; end?: string }) =>
  get<Kline[]>('/klines', params)
