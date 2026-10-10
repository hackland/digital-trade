import http, { get } from './http'

export interface TFState {
  interval: string
  candle_end: string
  close: number
  macd: number
  signal: number
  histogram: number
  trend: 'up' | 'down' | 'none'
  valid: boolean
}

export interface MtfEval {
  symbol: string
  h4: TFState
  h1: TFState
  m15: TFState
  htf_trend: 'up' | 'down' | 'none'
  cross_15m: 'up' | 'down' | ''
  zero_ok: boolean
  triggered: boolean
  direction: 'long' | 'short' | ''
  price: number
  time: string
  note: string
}

export interface MtfStatus {
  symbol: string
  eval: MtfEval | null
  updated_at: string
  error: string
  require_15m_beyond_zero: boolean
  macd: { fast: number; slow: number; signal: number }
}

export interface MtfSignal extends MtfEval {
  fired_at: string
}

export interface HorizonStat {
  samples: number
  win_rate: number
  avg_return_pct: number
  avg_win_pct: number
  avg_loss_pct: number
  sum_return_pct: number
}

export interface BtBucket {
  name: string
  count: number
  by_horizon: Record<string, HorizonStat>
  avg_mfe_pct: number
  avg_mae_pct: number
}

export interface Candle { t: number; o: number; h: number; l: number; c: number }
export interface Bar extends Candle { macd: number; signal: number; hist: number }
export interface MtfWindow { time: string; m15: Bar[]; h1: Bar[]; h4: Bar[] }

export interface BtOptions {
  days: number
  reversal: 'cross' | 'turn' | 'turn_pre'
  expanding_4h: boolean
  min_1h_dif_pct: number
  long_only: boolean
  divergence: 'none' | 'regular' | 'near' | 'hidden' | 'dif_up'
}

export interface BtSignal {
  h4_dif: number
  h1_dif: number
  m15_dif: number
  time: string
  direction: 'long' | 'short'
  price: number
  beyond_zero: boolean
  returns: Record<string, number | null>
  mfe_pct: number
  mae_pct: number
}

export interface MtfBacktest {
  symbol: string
  days: number
  bars_15m: number
  signals: BtSignal[]
  buckets: BtBucket[]
  horizons: number[]
  candles: Candle[]
  params: { LongOnly: boolean }
}

export const fetchMtfStatus = () => get<MtfStatus>('/mtf-macd/status')
export const fetchMtfSignals = () => get<MtfSignal[]>('/mtf-macd/signals')
export const setMtfConfig = (require_15m_beyond_zero: boolean) =>
  http.post('/mtf-macd/config', { require_15m_beyond_zero })
export const runMtfBacktest = async (opts: BtOptions) => {
  const body = { ...opts, divergence: opts.divergence === 'none' ? '' : opts.divergence }
  const res = await http.post('/mtf-macd/backtest', body, { timeout: 180000 })
  return res.data.data as MtfBacktest
}
export const fetchMtfWindow = async (time: string) => {
  const res = await http.get('/mtf-macd/window', { params: { time }, timeout: 60000 })
  return res.data.data as MtfWindow
}
