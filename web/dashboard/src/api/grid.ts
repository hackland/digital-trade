import http, { get } from './http'
import type { ApiResponse } from '@/types/api'

export interface GridBacktestRequest {
  symbol: string
  interval?: string
  days?: number
  start?: string
  end?: string
  upper_price?: number
  lower_price?: number
  auto_range_days?: number
  grid_spacing_pct: number
  usdt_per_grid: number
  cash?: number
  fee?: number
  stop_loss_below_pct?: number
  pause_buy_above_upper?: boolean
}

export type GridTradeSide = 'BUY' | 'SELL' | 'FORCE_CLOSE'

export interface GridTrade {
  timestamp: string
  side: GridTradeSide
  price: number
  quantity: number
  fee: number
  pnl: number
  slot_low: number
  slot_high: number
}

export interface GridEquityPoint {
  time: string
  equity: number
}

export interface GridMetrics {
  completed_round_trips: number
  realized_pnl: number
  total_fees: number
  remaining_qty: number
  remaining_value: number
  final_cash: number
  final_equity: number
  total_return_pct: number
  max_drawdown_pct: number
  max_concurrent_holdings: number
}

export interface GridResult {
  symbol: string
  interval: string
  start_time: string
  end_time: string
  initial_cash: number
  upper_price: number
  lower_price: number
  grid_spacing_pct: number
  grid_levels: number[]
  trades: GridTrade[]
  equity_curve: GridEquityPoint[]
  metrics: GridMetrics
  stopped_out: boolean
  stop_reason?: string
}

export interface SuggestRangeResponse {
  symbol: string
  days: number
  lower_price: number
  upper_price: number
  current_price: number
}

export async function runGridBacktest(req: GridBacktestRequest): Promise<GridResult> {
  const res = await http.post<ApiResponse<GridResult>>('/grid-backtest', req)
  return res.data.data
}

export function suggestGridRange(symbol: string, days: number): Promise<SuggestRangeResponse> {
  return get<SuggestRangeResponse>('/grid-backtest/suggest-range', { symbol, days })
}
