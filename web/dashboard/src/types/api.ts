export interface ApiResponse<T> {
  code: number
  message: string
  data: T
}

export interface PaginatedResponse<T> extends ApiResponse<T> {
  total: number
  limit: number
  offset: number
}
