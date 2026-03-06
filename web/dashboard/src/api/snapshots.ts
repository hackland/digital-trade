import { get } from './http'
import type { AccountSnapshot } from '@/types/models'

export const fetchSnapshots = (params?: Record<string, any>) =>
  get<AccountSnapshot[]>('/snapshots', params)
