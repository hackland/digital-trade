import { get } from './http'
import type { Overview } from '@/types/models'

export const fetchOverview = () => get<Overview>('/overview')
