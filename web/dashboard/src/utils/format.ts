export function formatNumber(n: number, decimals = 2): string {
  return n.toLocaleString('en-US', { minimumFractionDigits: decimals, maximumFractionDigits: decimals })
}

export function formatPrice(n: number): string {
  if (n >= 1000) return formatNumber(n, 2)
  if (n >= 1) return formatNumber(n, 4)
  return formatNumber(n, 6)
}

export function formatPnl(n: number): string {
  const prefix = n >= 0 ? '+' : ''
  return `${prefix}${formatNumber(n, 2)}`
}

export function formatPercent(n: number): string {
  const prefix = n >= 0 ? '+' : ''
  return `${prefix}${(n * 100).toFixed(2)}%`
}

export function formatTime(s: string): string {
  return new Date(s).toLocaleString()
}

export function formatShortTime(s: string): string {
  return new Date(s).toLocaleTimeString()
}
