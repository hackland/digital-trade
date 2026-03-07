<template>
  <div>
    <!-- Config Panel -->
    <el-card shadow="never" style="background: #1d1e1f; border-color: #333; margin-bottom: 16px">
      <template #header>
        <span style="color: #e0e0e0">Backtest Configuration</span>
      </template>
      <el-form :inline="true" size="small" label-position="top">
        <el-form-item label="Symbol">
          <el-select v-model="form.symbol" style="width: 130px">
            <el-option v-for="s in SYMBOLS" :key="s" :label="s" :value="s" />
          </el-select>
        </el-form-item>
        <el-form-item label="Interval">
          <el-select v-model="form.interval" style="width: 100px">
            <el-option v-for="i in INTERVALS" :key="i" :label="i" :value="i" />
          </el-select>
        </el-form-item>
        <el-form-item label="Strategy">
          <el-select v-model="form.strategy" style="width: 200px">
            <el-option v-for="s in strategies" :key="s.name" :label="s.label" :value="s.name" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="isManualComposite" label="Price Strategy">
          <el-select v-model="form.price_strategy" style="width: 190px">
            <el-option v-for="s in priceStrategyOptions" :key="s.name" :label="s.label" :value="s.name" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="isManualComposite" label="Volume Strategy">
          <el-select v-model="form.volume_strategy" style="width: 210px">
            <el-option v-for="s in volumeStrategyOptions" :key="s.name" :label="s.label" :value="s.name" />
          </el-select>
        </el-form-item>
        <el-form-item label="Initial Cash">
          <el-input-number v-model="form.cash" :min="100" :step="1000" style="width: 140px" />
        </el-form-item>
        <el-form-item label="Alloc %">
          <el-input-number v-model="allocPct" :min="1" :max="100" :step="5" style="width: 100px" />
        </el-form-item>
        <el-form-item label="Fee %">
          <el-input-number v-model="feePct" :min="0" :max="100" :step="0.01" :precision="2" style="width: 110px" />
        </el-form-item>
      </el-form>

      <!-- Time Range Row -->
      <div class="time-range-row">
        <div class="quick-btns">
          <el-button
            v-for="preset in dayPresets"
            :key="preset.days"
            :type="activeDays === preset.days && !useCustomRange ? 'primary' : 'default'"
            size="small"
            @click="selectPreset(preset.days)"
            :style="activeDays === preset.days && !useCustomRange
              ? 'background: #f0b90b; border-color: #f0b90b; color: #000'
              : 'background: #252526; border-color: #444; color: #b0b0b0'"
          >
            {{ preset.label }}
          </el-button>
          <el-button
            :type="useCustomRange ? 'primary' : 'default'"
            size="small"
            @click="useCustomRange = true"
            :style="useCustomRange
              ? 'background: #f0b90b; border-color: #f0b90b; color: #000'
              : 'background: #252526; border-color: #444; color: #b0b0b0'"
          >
            Custom
          </el-button>
        </div>

        <el-date-picker
          v-if="useCustomRange"
          v-model="dateRange"
          type="daterange"
          range-separator="~"
          start-placeholder="Start"
          end-placeholder="End"
          format="YYYY-MM-DD"
          value-format="YYYY-MM-DD"
          size="small"
          style="width: 280px; margin-left: 12px"
          :disabled-date="disableFutureDate"
        />

        <el-button
          type="primary"
          :loading="loading"
          @click="doBacktest"
          size="small"
          style="background: #f0b90b; border-color: #f0b90b; color: #000; margin-left: 16px; height: 32px; font-weight: 600"
        >
          Run Backtest
        </el-button>
      </div>
    </el-card>

    <!-- Results -->
    <template v-if="result">
      <!-- Metrics Summary -->
      <el-row :gutter="16" style="margin-bottom: 16px">
        <el-col :span="6">
          <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
            <div class="metric-card">
              <div class="metric-label">Total Return</div>
              <div class="metric-value" :style="{ color: result.metrics.total_return >= 0 ? '#67C23A' : '#F56C6C' }">
                {{ result.metrics.total_return >= 0 ? '+' : '' }}{{ result.metrics.total_return.toFixed(2) }} USDT
              </div>
              <div class="metric-sub">
                {{ result.metrics.total_return_pct >= 0 ? '+' : '' }}{{ result.metrics.total_return_pct.toFixed(2) }}%
                · {{ result.metrics.total_trades }} trades
              </div>
            </div>
          </el-card>
        </el-col>
        <el-col :span="6">
          <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
            <div class="metric-card">
              <div class="metric-label">Win Rate</div>
              <div class="metric-value" style="color: #f0b90b">{{ (result.metrics.win_rate * 100).toFixed(1) }}%</div>
              <div class="metric-sub">{{ result.metrics.win_trades }}W / {{ result.metrics.lose_trades }}L ({{ result.metrics.total_trades }} total)</div>
            </div>
          </el-card>
        </el-col>
        <el-col :span="6">
          <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
            <div class="metric-card">
              <div class="metric-label">Max Drawdown</div>
              <div class="metric-value" style="color: #F56C6C">-{{ result.metrics.max_drawdown_pct.toFixed(2) }}%</div>
              <div class="metric-sub">${{ result.metrics.max_drawdown.toFixed(2) }}</div>
            </div>
          </el-card>
        </el-col>
        <el-col :span="6">
          <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
            <div class="metric-card">
              <div class="metric-label">Sharpe / Sortino</div>
              <div class="metric-value" style="color: #e0e0e0">{{ result.metrics.sharpe_ratio.toFixed(2) }} / {{ result.metrics.sortino_ratio.toFixed(2) }}</div>
              <div class="metric-sub">Profit Factor: {{ result.metrics.profit_factor.toFixed(2) }}</div>
            </div>
          </el-card>
        </el-col>
      </el-row>

      <el-card shadow="never" style="background: #1d1e1f; border-color: #333; margin-bottom: 16px">
        <el-tabs v-model="activeChartTab" class="bt-chart-tabs">
          <el-tab-pane label="Backtest Kline" name="kline">
            <div ref="klineChartContainer" style="width: 100%; height: 420px"></div>
          </el-tab-pane>
          <el-tab-pane label="Equity Curve" name="equity">
            <div ref="equityChartContainer" style="width: 100%; height: 350px"></div>
          </el-tab-pane>
        </el-tabs>
      </el-card>

      <!-- Detail Metrics + Trades -->
      <el-row :gutter="16">
        <!-- Detail Stats -->
        <el-col :span="8">
          <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
            <template #header>
              <span style="color: #e0e0e0">Performance Details</span>
            </template>
            <div class="detail-grid">
              <div class="detail-row"><span>Initial Cash</span><span>${{ result.initial_cash.toFixed(2) }}</span></div>
              <div class="detail-row"><span>Final Equity</span><span>${{ result.metrics.final_equity.toFixed(2) }}</span></div>
              <div class="detail-row"><span>Total Fees</span><span>${{ result.metrics.total_fees.toFixed(2) }}</span></div>
              <div class="detail-row"><span>Avg Win</span><span style="color: #67C23A">${{ result.metrics.avg_win.toFixed(2) }}</span></div>
              <div class="detail-row"><span>Avg Loss</span><span style="color: #F56C6C">${{ result.metrics.avg_loss.toFixed(2) }}</span></div>
              <div class="detail-row"><span>Largest Win</span><span style="color: #67C23A">${{ result.metrics.largest_win.toFixed(2) }}</span></div>
              <div class="detail-row"><span>Largest Loss</span><span style="color: #F56C6C">${{ result.metrics.largest_loss.toFixed(2) }}</span></div>
              <div class="detail-row"><span>Period</span><span>{{ formatDate(result.start_time) }} ~ {{ formatDate(result.end_time) }}</span></div>
            </div>
          </el-card>
        </el-col>

        <!-- Trade List -->
        <el-col :span="16">
          <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
            <template #header>
              <span style="color: #e0e0e0">Trade History ({{ result.trades?.length || 0 }})</span>
            </template>
            <el-table
              :data="result.trades"
              style="width: 100%"
              size="small"
              max-height="400"
              :header-cell-style="{ background: '#252526', color: '#b0b0b0' }"
              :cell-style="tradeCellStyle"
              @row-click="onTradeRowClick"
            >
              <el-table-column label="Time" width="160">
                <template #default="{ row }">{{ formatTime(row.timestamp) }}</template>
              </el-table-column>
              <el-table-column prop="side" label="Side" width="70">
                <template #default="{ row }">
                  <el-tag :type="row.side === 'BUY' ? 'success' : 'danger'" size="small">{{ row.side }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column label="Price" width="100">
                <template #default="{ row }">{{ formatPrice(row.price) }}</template>
              </el-table-column>
              <el-table-column label="Qty" width="100">
                <template #default="{ row }">{{ row.quantity.toFixed(6) }}</template>
              </el-table-column>
              <el-table-column label="Amount (U)" width="110">
                <template #default="{ row }">
                  <span :style="{ color: row.side === 'BUY' ? '#67C23A' : '#F56C6C' }">
                    {{ row.side === 'BUY' ? '-' : '+' }}{{ (row.price * row.quantity).toFixed(2) }}
                  </span>
                </template>
              </el-table-column>
              <el-table-column label="Fee (U)" width="80">
                <template #default="{ row }">{{ row.fee.toFixed(2) }}</template>
              </el-table-column>
              <el-table-column label="PnL (U)" width="100">
                <template #default="{ row }">
                  <span v-if="row.side === 'SELL'" :style="{ color: row.pnl >= 0 ? '#67C23A' : '#F56C6C' }">
                    {{ row.pnl >= 0 ? '+' : '' }}{{ row.pnl.toFixed(2) }}
                  </span>
                  <span v-else style="color: #888">-</span>
                </template>
              </el-table-column>
              <el-table-column label="Reason" min-width="200" show-overflow-tooltip>
                <template #default="{ row }">{{ row.reason }}</template>
              </el-table-column>
            </el-table>
          </el-card>
        </el-col>
      </el-row>
    </template>

    <!-- Empty State -->
    <el-card v-else-if="!loading" shadow="never" style="background: #1d1e1f; border-color: #333; text-align: center; padding: 60px 0">
      <el-empty description="Configure parameters and click 'Run Backtest' to start" />
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, nextTick, watch } from 'vue'
import { createChart, type IChartApi, type ISeriesApi, ColorType } from 'lightweight-charts'
import { runBacktest as apiRunBacktest, getStrategies, type BacktestRequest, type BacktestResult, type StrategyInfo } from '@/api/backtest'
import { fetchKlines } from '@/api/klines'
import { SYMBOLS, INTERVALS } from '@/utils/constants'
import { formatPrice, formatTime } from '@/utils/format'
import { ElMessage } from 'element-plus'

// --- Day presets ---
const dayPresets = [
  { label: '7D', days: 7 },
  { label: '30D', days: 30 },
  { label: '90D', days: 90 },
  { label: '180D', days: 180 },
  { label: '365D', days: 365 },
]

const form = ref({
  symbol: 'BTCUSDT',
  interval: '5m',
  strategy: 'ema_crossover',
  price_strategy: 'ema_crossover',
  volume_strategy: 'volume_trend',
  cash: 10000,
})

const activeDays = ref(30)
const useCustomRange = ref(false)
const dateRange = ref<[string, string] | null>(null)

const allocPct = ref(10)
const feePct = ref(0.1)
const loading = ref(false)
const result = ref<BacktestResult | null>(null)
const selectedTradeIndex = ref<number | null>(null)
const strategies = ref<StrategyInfo[]>([
  { name: 'ema_crossover', label: 'EMA Crossover' },
  { name: 'macd_rsi', label: 'MACD + RSI' },
  { name: 'bb_breakout', label: 'Bollinger Bands Breakout' },
  { name: 'vwap_reversion', label: 'VWAP Mean Reversion' },
  { name: 'volume_trend', label: 'Volume Trend' },
  { name: 'composite_score', label: 'Composite Score (Multi-Indicator)' },
  { name: 'manual_composite', label: 'Manual Composite (Price + Volume)' },
])
const priceStrategyOptions: StrategyInfo[] = [
  { name: 'ema_crossover', label: 'EMA Crossover' },
  { name: 'macd_rsi', label: 'MACD + RSI' },
  { name: 'bb_breakout', label: 'Bollinger Bands Breakout' },
]
const volumeStrategyOptions: StrategyInfo[] = [
  { name: 'volume_trend', label: 'Volume Trend' },
  { name: 'vwap_reversion', label: 'VWAP Mean Reversion' },
  { name: 'composite_score', label: 'Composite Score (Multi-Indicator)' },
]
const isManualComposite = ref(false)

const activeChartTab = ref<'kline' | 'equity'>('kline')
const equityChartContainer = ref<HTMLElement | null>(null)
const klineChartContainer = ref<HTMLElement | null>(null)
let equityChart: IChartApi | null = null
let klineChart: IChartApi | null = null
let equitySeries: ISeriesApi<'Area'> | null = null
let klineCandleSeries: ISeriesApi<'Candlestick'> | null = null
let klineVolumeSeries: ISeriesApi<'Histogram'> | null = null
let equityResizeObserver: ResizeObserver | null = null
let klineResizeObserver: ResizeObserver | null = null
let backtestTradeMarkers: any[] = []

function selectPreset(days: number) {
  activeDays.value = days
  useCustomRange.value = false
  dateRange.value = null
}

function disableFutureDate(date: Date): boolean {
  return date.getTime() > Date.now()
}

function formatDate(s: string): string {
  const d = new Date(s)
  if (d.getFullYear() <= 1) return 'N/A'
  return d.toLocaleDateString()
}

async function doBacktest() {
  loading.value = true
  result.value = null
  selectedTradeIndex.value = null
  backtestTradeMarkers = []
  try {
    const req: BacktestRequest = {
      ...form.value,
      alloc: allocPct.value / 100,
      fee: feePct.value / 100,
    }
    if (form.value.strategy !== 'manual_composite') {
      delete req.price_strategy
      delete req.volume_strategy
    }

    if (useCustomRange.value && dateRange.value) {
      req.start = dateRange.value[0]
      req.end = dateRange.value[1]
    } else {
      req.days = activeDays.value
    }

    const res = await apiRunBacktest(req)
    result.value = res
    await nextTick()
    await renderActiveChart()
    ElMessage.success(`Backtest complete: ${res.metrics.total_trades} trades, ${res.metrics.total_return_pct.toFixed(2)}% return`)
  } catch (e: any) {
    ElMessage.error('Backtest failed: ' + (e.response?.data?.message || e.message))
  } finally {
    loading.value = false
  }
}

const tzOffsetSec = -new Date().getTimezoneOffset() * 60

function toLocalChartTime(isoOrTs: string | number): number {
  const utcSec = Math.floor(new Date(isoOrTs).getTime() / 1000)
  return utcSec + tzOffsetSec
}

function volumeColor(open: number, close: number): string {
  return close >= open ? 'rgba(103,194,58,0.4)' : 'rgba(245,108,108,0.4)'
}

function intervalSeconds(interval: string): number {
  switch (interval) {
    case '1m': return 60
    case '3m': return 180
    case '5m': return 300
    case '15m': return 900
    case '30m': return 1800
    case '1h': return 3600
    case '2h': return 7200
    case '4h': return 14400
    case '6h': return 21600
    case '8h': return 28800
    case '12h': return 43200
    case '1d': return 86400
    default: return 300
  }
}

function cleanupEquityChart() {
  if (equityResizeObserver && equityChartContainer.value) {
    equityResizeObserver.unobserve(equityChartContainer.value)
    equityResizeObserver.disconnect()
  }
  equityResizeObserver = null
  equityChart?.remove()
  equityChart = null
  equitySeries = null
}

function cleanupKlineChart() {
  if (klineResizeObserver && klineChartContainer.value) {
    klineResizeObserver.unobserve(klineChartContainer.value)
    klineResizeObserver.disconnect()
  }
  klineResizeObserver = null
  klineChart?.remove()
  klineChart = null
  klineCandleSeries = null
  klineVolumeSeries = null
  backtestTradeMarkers = []
}

function buildTradeMarkers() {
  if (!result.value?.trades?.length) {
    backtestTradeMarkers = []
    return
  }
  backtestTradeMarkers = result.value.trades.map((t, idx) => {
    const isBuy = t.side === 'BUY'
    return {
      id: `${t.side}-${idx}`,
      time: toLocalChartTime(t.timestamp) as any,
      position: (isBuy ? 'belowBar' : 'aboveBar') as any,
      color: isBuy ? '#67C23A' : '#F56C6C',
      shape: (isBuy ? 'arrowUp' : 'arrowDown') as any,
      text: isBuy ? 'B' : 'S',
    }
  })
}

function applyTradeMarkers() {
  if (!klineCandleSeries) return
  const selected = selectedTradeIndex.value
  const markers = backtestTradeMarkers.map((m, idx) => {
    if (selected === idx) {
      return {
        ...m,
        color: m.shape === 'arrowUp' ? '#22c55e' : '#ef4444',
        text: `${m.text}*`,
      }
    }
    return m
  })
  klineCandleSeries.setMarkers(markers)
}

function onTradeRowClick(row: BacktestResult['trades'][number]) {
  if (!result.value?.trades?.length) return
  const idx = result.value.trades.indexOf(row)
  if (idx < 0) return
  selectedTradeIndex.value = idx
  activeChartTab.value = 'kline'
  nextTick(async () => {
    if (!klineCandleSeries) {
      await renderBacktestKlineChart()
    } else {
      applyTradeMarkers()
    }
  })
}

function tradeCellStyle({ rowIndex }: { rowIndex: number }) {
  if (selectedTradeIndex.value === rowIndex) {
    return { background: '#2a2412', color: '#f0b90b' }
  }
  return { background: '#1d1e1f', color: '#e0e0e0' }
}

async function renderActiveChart() {
  if (!result.value) return
  if (activeChartTab.value === 'kline') {
    await renderBacktestKlineChart()
  } else {
    renderEquityChart()
  }
}

function renderEquityChart() {
  if (!equityChartContainer.value || !result.value?.equity_curve?.length) return
  cleanupEquityChart()

  equityChart = createChart(equityChartContainer.value, {
    width: equityChartContainer.value.clientWidth,
    height: 350,
    layout: {
      background: { type: ColorType.Solid, color: '#1d1e1f' },
      textColor: '#b0b0b0',
    },
    grid: {
      vertLines: { color: '#2a2a2a' },
      horzLines: { color: '#2a2a2a' },
    },
    timeScale: { timeVisible: true, secondsVisible: false },
    rightPriceScale: { borderColor: '#333' },
  })

  equitySeries = equityChart.addAreaSeries({
    lineColor: '#f0b90b',
    topColor: 'rgba(240, 185, 11, 0.3)',
    bottomColor: 'rgba(240, 185, 11, 0.02)',
    lineWidth: 2,
    priceFormat: { type: 'price', precision: 2, minMove: 0.01 },
  })

  const data = result.value.equity_curve.map(p => ({
    time: toLocalChartTime(p.time) as any,
    value: p.equity,
  }))
  equitySeries.setData(data)
  equityChart.timeScale().fitContent()

  equityResizeObserver = new ResizeObserver(() => {
    if (equityChart && equityChartContainer.value) {
      equityChart.applyOptions({ width: equityChartContainer.value.clientWidth })
    }
  })
  equityResizeObserver.observe(equityChartContainer.value)
}

async function renderBacktestKlineChart() {
  if (!klineChartContainer.value || !result.value) return
  cleanupKlineChart()

  klineChart = createChart(klineChartContainer.value, {
    width: klineChartContainer.value.clientWidth,
    height: 420,
    layout: {
      background: { type: ColorType.Solid, color: '#1d1e1f' },
      textColor: '#b0b0b0',
    },
    grid: {
      vertLines: { color: '#2a2a2a' },
      horzLines: { color: '#2a2a2a' },
    },
    crosshair: { mode: 0 },
    timeScale: { timeVisible: true, secondsVisible: false },
    rightPriceScale: { borderColor: '#333' },
  })

  klineCandleSeries = klineChart.addCandlestickSeries({
    upColor: '#67C23A',
    downColor: '#F56C6C',
    borderUpColor: '#67C23A',
    borderDownColor: '#F56C6C',
    wickUpColor: '#67C23A',
    wickDownColor: '#F56C6C',
    priceScaleId: 'right',
  })
  klineCandleSeries.priceScale().applyOptions({
    scaleMargins: { top: 0.05, bottom: 0.3 },
  })

  klineVolumeSeries = klineChart.addHistogramSeries({
    priceFormat: { type: 'volume' },
    priceScaleId: 'volume',
  })
  klineVolumeSeries.priceScale().applyOptions({
    scaleMargins: { top: 0.75, bottom: 0 },
  })

  const startMs = new Date(result.value.start_time).getTime()
  const endMs = new Date(result.value.end_time).getTime()
  const intervalSec = intervalSeconds(result.value.interval)
  const estimatedBars = Math.ceil((endMs - startMs) / 1000 / intervalSec)
  const limit = Math.min(Math.max(estimatedBars + 10, 800), 10000)

  const klines = await fetchKlines({
    symbol: result.value.symbol,
    interval: result.value.interval,
    start: result.value.start_time,
    end: result.value.end_time,
    limit,
  })

  const candleData: any[] = []
  const volData: any[] = []
  for (const k of klines) {
    const t = toLocalChartTime(k.open_time)
    candleData.push({
      time: t as any,
      open: k.open,
      high: k.high,
      low: k.low,
      close: k.close,
    })
    volData.push({
      time: t as any,
      value: k.volume,
      color: volumeColor(k.open, k.close),
    })
  }

  klineCandleSeries.setData(candleData)
  klineVolumeSeries.setData(volData)

  buildTradeMarkers()
  applyTradeMarkers()
  klineChart.timeScale().fitContent()

  if (estimatedBars > 10000) {
    ElMessage.warning(`K线数量较多（约 ${estimatedBars} 根），图表已限制到 10000 根以保证性能。`)
  }

  klineResizeObserver = new ResizeObserver(() => {
    if (klineChart && klineChartContainer.value) {
      klineChart.applyOptions({ width: klineChartContainer.value.clientWidth })
    }
  })
  klineResizeObserver.observe(klineChartContainer.value)
}

onMounted(async () => {
  try {
    const list = await getStrategies()
    if (list?.length) strategies.value = list
  } catch {}
  isManualComposite.value = form.value.strategy === 'manual_composite'
})

watch(() => form.value.strategy, (next) => {
  isManualComposite.value = next === 'manual_composite'
  if (next === 'manual_composite') {
    if (!form.value.price_strategy) form.value.price_strategy = 'ema_crossover'
    if (!form.value.volume_strategy) form.value.volume_strategy = 'volume_trend'
  }
})

watch(activeChartTab, async () => {
  await nextTick()
  await renderActiveChart()
})

onBeforeUnmount(() => {
  cleanupEquityChart()
  cleanupKlineChart()
})
</script>

<style scoped>
.time-range-row {
  display: flex;
  align-items: center;
  margin-top: 8px;
  padding-top: 12px;
  border-top: 1px solid #333;
}

.quick-btns {
  display: flex;
  gap: 6px;
}

.bt-chart-tabs :deep(.el-tabs__item) {
  color: #b0b0b0;
}

.bt-chart-tabs :deep(.el-tabs__item.is-active) {
  color: #f0b90b;
}

.metric-card {
  text-align: center;
  padding: 8px 0;
}
.metric-label {
  color: #888;
  font-size: 12px;
  margin-bottom: 4px;
}
.metric-value {
  font-size: 24px;
  font-weight: 600;
}
.metric-sub {
  color: #888;
  font-size: 12px;
  margin-top: 4px;
}

.detail-grid {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.detail-row {
  display: flex;
  justify-content: space-between;
  color: #e0e0e0;
  font-size: 13px;
}
.detail-row span:first-child {
  color: #888;
}

:deep(.el-form-item__label) {
  color: #b0b0b0 !important;
  font-size: 12px !important;
}
:deep(.el-input__inner),
:deep(.el-input-number__decrease),
:deep(.el-input-number__increase) {
  background: #252526 !important;
  color: #e0e0e0 !important;
  border-color: #444 !important;
}
:deep(.el-select .el-input__inner) {
  background: #252526 !important;
  color: #e0e0e0 !important;
}
:deep(.el-empty__description p) {
  color: #888 !important;
}
/* Date picker dark theme */
:deep(.el-date-editor) {
  --el-date-editor-width: 280px;
}
:deep(.el-range-input) {
  background: transparent !important;
  color: #e0e0e0 !important;
}
:deep(.el-range-separator) {
  color: #888 !important;
}
:deep(.el-date-editor .el-range-input) {
  color: #e0e0e0 !important;
}
</style>
