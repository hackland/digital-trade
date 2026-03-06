import { get, getPaginated } from './http'
import type { OrderRecord } from '@/types/models'
import type { PaginatedResponse } from '@/types/api'

export const fetchOrders = (params?: Record<string, any>): Promise<PaginatedResponse<OrderRecord[]>> =>
  getPaginated<OrderRecord[]>('/orders', params)

export const fetchActiveOrders = () => get<any[]>('/orders/active')
export const fetchOrder = (id: number) => get<OrderRecord>(`/orders/${id}`)
