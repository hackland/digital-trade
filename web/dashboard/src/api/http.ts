import axios from 'axios'
import type { ApiResponse, PaginatedResponse } from '@/types/api'

const http = axios.create({
  baseURL: '/api/v1',
  timeout: 10000,
})

http.interceptors.response.use(
  (res) => res,
  (err) => {
    console.error('API error:', err.response?.data || err.message)
    return Promise.reject(err)
  }
)

export async function get<T>(url: string, params?: Record<string, any>): Promise<T> {
  const res = await http.get<ApiResponse<T>>(url, { params })
  return res.data.data
}

export async function getPaginated<T>(url: string, params?: Record<string, any>): Promise<PaginatedResponse<T>> {
  const res = await http.get<PaginatedResponse<T>>(url, { params })
  return res.data
}

export default http
