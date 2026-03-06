import http from './http'
import type { ApiResponse } from '@/types/api'

export interface BacktestRequest {
  symbol: string
  interval: string
  strategy: string
  days?: number
  start?: string
  end?: string
  cash?: number
  fee?: number
  alloc?: number
}

export interface BacktestMetrics {
  final_equity: number
  total_return: number
  total_return_pct: number
  total_trades: number
  win_trades: number
  lose_trades: number
  win_rate: number
  avg_win: number
  avg_loss: number
  largest_win: number
  largest_loss: number
  profit_factor: number
  max_drawdown: number
  max_drawdown_pct: number
  sharpe_ratio: number
  sortino_ratio: number
  total_fees: number
  annualized_return: number
}

export interface TradeRecord {
  timestamp: string
  side: string
  price: number
  quantity: number
  fee: number
  pnl: number
  reason: string
}

export interface EquityPoint {
  time: string
  equity: number
}

export interface BacktestResult {
  symbol: string
  strategy: string
  interval: string
  start_time: string
  end_time: string
  duration: number
  initial_cash: number
  trades: TradeRecord[]
  metrics: BacktestMetrics
  equity_curve: EquityPoint[]
}

export interface StrategyInfo {
  name: string
  label: string
}

export async function runBacktest(req: BacktestRequest): Promise<BacktestResult> {
  const res = await http.post<ApiResponse<BacktestResult>>('/backtest', req)
  return res.data.data
}

export async function getStrategies(): Promise<StrategyInfo[]> {
  const res = await http.get<ApiResponse<StrategyInfo[]>>('/backtest/strategies')
  return res.data.data
}
