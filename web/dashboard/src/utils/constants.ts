export const SYMBOLS = ['BTCUSDT', 'ETHUSDT']
export const INTERVALS = ['1m', '5m', '15m', '1h', '4h', '1d']

export const SIDE_COLORS = {
  BUY: '#67C23A',
  SELL: '#F56C6C',
} as const

export const STATUS_TYPES: Record<string, string> = {
  NEW: 'primary',
  PARTIALLY_FILLED: 'warning',
  FILLED: 'success',
  CANCELED: 'info',
  REJECTED: 'danger',
  EXPIRED: 'info',
}
