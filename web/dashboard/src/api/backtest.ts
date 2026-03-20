import http from './http'
import type { ApiResponse } from '@/types/api'

export interface BacktestRequest {
  symbol: string
  interval: string
  strategy: string
  price_strategy?: string
  volume_strategy?: string
  strategy_config?: Record<string, any>
  days?: number
  start?: string
  end?: string
  cash?: number
  fee?: number
  alloc?: number
}

// --- Indicator Module Types ---

export interface ParamSchema {
  key: string
  label: string
  type: 'int' | 'float' | 'bool' | 'string'
  default: any
  min: number
  max: number
  step: number
  group?: string  // "signal" | "position" | "stoploss" | "trend" | "short"
  desc?: string   // tooltip help text
}

export interface ModuleMeta {
  name: string
  label: string
  category: string
  description: string
  default_weight: number
  params: ParamSchema[]
}

export interface SignalPreset {
  label: string
  desc: string
  [key: string]: any
}

export interface IndicatorModulesResponse {
  modules: ModuleMeta[]
  grouped: Record<string, ModuleMeta[]>
  signal_params: ParamSchema[]
  signal_presets?: Record<string, SignalPreset>
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
  fee_rate: number
  alloc_pct: number
  trades: TradeRecord[]
  metrics: BacktestMetrics
  equity_curve: EquityPoint[]
  short_trades: TradeRecord[]
  short_metrics: BacktestMetrics
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

export async function getIndicatorModules(): Promise<IndicatorModulesResponse> {
  const res = await http.get<ApiResponse<IndicatorModulesResponse>>('/indicator/modules')
  return res.data.data
}

export interface DeployRequest {
  modules: { name: string; weight: number }[]
  signal_params: Record<string, any>
}

export interface DeployResponse {
  message: string
  config: Record<string, any>
}

export async function deployStrategy(req: DeployRequest): Promise<DeployResponse> {
  const res = await http.post<ApiResponse<DeployResponse>>('/strategy/deploy', req)
  return res.data.data
}

export interface StrategyDiagnostics {
  message?: string  // when no eval has happened yet
  timestamp: string
  symbol: string
  action: string
  composite_score: number
  module_scores: Record<string, number>
  module_weights: Record<string, number>
  buy_threshold: number
  sell_threshold: number
  has_position: boolean
  entry_price: number
  high_water_mark: number
  bars_since_entry: number
  confirm_count: number
  confirm_bars: number
  cooldown_count: number
  cooldown_bars: number
  min_hold_bars: number
  trend_filter_on: boolean
  trend_bullish: boolean
  trend_ema_dist_pct: number
  htf_enabled: boolean
  htf_bullish: boolean
  htf_blocked: boolean
  htf_ema_dist_pct: number
  atr_stop_mult: number
  atr_value: number
  stop_price: number
  close_price: number
  hold_reason: string
  reason: string
}

export async function getStrategyDiagnostics(): Promise<StrategyDiagnostics> {
  const res = await http.get<ApiResponse<StrategyDiagnostics>>('/strategy/diagnostics')
  return res.data.data
}
