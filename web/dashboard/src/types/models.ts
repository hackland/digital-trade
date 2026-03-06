export interface Position {
  symbol: string
  quantity: number
  avg_entry_price: number
  current_price: number
  unrealized_pnl: number
  realized_pnl: number
  side: string
}

export interface Overview {
  total_equity: number
  free_cash: number
  position_value: number
  unrealized_pnl: number
  realized_pnl: number
  daily_pnl: number
  drawdown_pct: number
  is_trading_paused: boolean
  positions: Position[]
}

export interface OrderRecord {
  id: number
  exchange_id: number
  client_order_id: string
  symbol: string
  side: string
  type: string
  status: string
  price: number
  quantity: number
  filled_qty: number
  avg_price: number
  strategy_name: string
  signal_reason: string
  created_at: string
  updated_at: string
}

export interface TradeRecord {
  id: number
  exchange_id: number
  order_id: number
  symbol: string
  side: string
  price: number
  quantity: number
  fee: number
  fee_asset: string
  strategy_name: string
  realized_pnl: number
  timestamp: string
}

export interface SignalRecord {
  id: number
  timestamp: string
  symbol: string
  strategy_name: string
  action: string
  strength: number
  reason: string
  indicators: Record<string, number>
  was_executed: boolean
}

export interface AccountSnapshot {
  timestamp: string
  total_equity: number
  free_cash: number
  position_value: number
  unrealized_pnl: number
  realized_pnl: number
  daily_pnl: number
  drawdown_pct: number
}

export interface RiskStatus {
  daily_pnl: number
  daily_pnl_pct: number
  current_drawdown: number
  max_drawdown: number
  peak_equity: number
  current_equity: number
  daily_trade_count: number
  is_trading_paused: boolean
  pause_reason: string
  pause_until: string
}

export interface Kline {
  symbol: string
  interval: string
  open_time: string
  close_time: string
  open: number
  high: number
  low: number
  close: number
  volume: number
  quote_volume: number
  trades: number
  is_final: boolean
}

export interface Ticker {
  symbol: string
  bid_price: number
  ask_price: number
  last_price: number
  volume_24h: number
}
