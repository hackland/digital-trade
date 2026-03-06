import { get } from './http'
import type { Position } from '@/types/models'

export const fetchPositions = () => get<Position[]>('/positions')
export const fetchPosition = (symbol: string) => get<Position>(`/positions/${symbol}`)
