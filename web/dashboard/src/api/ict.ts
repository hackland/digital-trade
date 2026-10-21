import http, { get } from './http'

export interface IctParams {
  swing_n: number
  liq_lookback: number
  equal_tol_pct: number
  disp_atr: number
  max_sweep_to_mss: number
  min_fvg_atr: number
  entry_depth: number
  stop_buf_atr: number
  min_rr: number
  max_risk_pct: number
  min_risk_pct: number
  require_bias: boolean
  require_discount: boolean
  breakout: boolean
  direction: 'both' | 'long' | 'short'
  expiry_bars: number
  max_hold_bars: number
  fee_rate: number
}

export type IctStatus = 'pending' | 'open' | 'tp' | 'sl' | 'timeout' | 'expired' | 'missed' | 'invalid'

export interface IctSetup {
  id: string
  model: 'reversal' | 'breakout'
  dir: 'long' | 'short'
  bias: 'up' | 'down' | 'none'
  sweep_time: string
  sweep_kind: string
  sweep_level: number
  sweep_extreme: number
  mss_time: string
  mss_level: number
  zone_kind: 'FVG' | 'OB'
  zone_time: string
  zone_top: number
  zone_bottom: number
  arm_time: string
  entry: number
  stop: number
  tp: number
  tp_kind: string
  planned_rr: number
  status: IctStatus
  fill_time?: string
  fill_price?: number
  exit_time?: string
  exit_price?: number
  r: number
  end_time: string
}

export interface IctCandle { t: number; o: number; h: number; l: number; c: number }

export interface IctStats {
  armed: number
  filled: number
  resolved: number
  open: number
  pending: number
  tp: number
  sl: number
  timeout: number
  expired: number
  missed: number
  invalid: number
  wins: number
  win_rate: number
  fill_rate: number
  avg_r: number
  total_r: number
  avg_win_r: number
  avg_loss_r: number
  profit_factor: number
  max_drawdown_r: number
  avg_planned_rr: number
}

export interface IctBucket { group: string; name: string; count: number; win_rate: number; avg_r: number; total_r: number }

export interface IctResult {
  symbol: string
  days: number
  params: IctParams
  generated_at: string
  last_price: number
  bias_now: 'up' | 'down' | 'none'
  candles: IctCandle[]
  setups: IctSetup[]
  stats: IctStats
  buckets: IctBucket[]
  equity: { t: number; r: number }[]
  rejected: Record<string, number>
  funnel: { sweeps: number; shifts: number; breakouts: number; armed: number }
  bars_1h: number
  bars_15m: number
}

export const defaultIctParams = (): IctParams => ({
  swing_n: 3,
  liq_lookback: 72,
  equal_tol_pct: 0.1,
  disp_atr: 1.0,
  max_sweep_to_mss: 24,
  min_fvg_atr: 0.1,
  entry_depth: 0.5,
  stop_buf_atr: 0.25,
  min_rr: 2,
  max_risk_pct: 3,
  min_risk_pct: 0.15,
  require_bias: true,
  require_discount: false,
  breakout: false,
  direction: 'both',
  expiry_bars: 96,
  max_hold_bars: 288,
  fee_rate: 0.001,
})

export const fetchIctScan = (days = 14) =>
  get<IctResult>('/ict/scan', { days })

export const runIctBacktest = async (days: number, params: Partial<IctParams>) => {
  const res = await http.post('/ict/backtest', { days, ...params }, { timeout: 180000 })
  return res.data.data as IctResult
}
