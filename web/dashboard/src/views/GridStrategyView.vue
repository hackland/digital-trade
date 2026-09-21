<template>
  <div>
    <!-- Config Panel -->
    <el-card shadow="never" style="background: #1d1e1f; border-color: #333; margin-bottom: 16px">
      <template #header>
        <span style="color: #e0e0e0">Grid Backtest Configuration</span>
      </template>
      <el-form :inline="true" size="small" label-position="top">
        <el-form-item label="Symbol">
          <el-select v-model="form.symbol" style="width: 130px">
            <el-option v-for="s in SYMBOLS" :key="s" :label="s" :value="s" />
          </el-select>
        </el-form-item>
        <el-form-item label="Interval">
          <el-select v-model="form.interval" style="width: 100px">
            <el-option label="1m" value="1m" />
            <el-option label="5m" value="5m" />
          </el-select>
        </el-form-item>
        <el-form-item label="Initial Cash">
          <el-input-number v-model="form.cash" :min="100" :step="1000" style="width: 140px" />
        </el-form-item>
        <el-form-item label="Fee %">
          <el-input-number v-model="feePct" :min="0" :max="100" :step="0.01" :precision="2" style="width: 110px" />
        </el-form-item>
      </el-form>

      <div class="grid-params-row">
        <el-form :inline="true" size="small" label-position="top">
          <el-form-item label="Lower Price">
            <el-input-number v-model="form.lower_price" :min="0" :step="100" :precision="2" style="width: 150px" />
          </el-form-item>
          <el-form-item label="Upper Price">
            <el-input-number v-model="form.upper_price" :min="0" :step="100" :precision="2" style="width: 150px" />
          </el-form-item>
          <el-form-item label=" ">
            <el-button size="small" :loading="suggesting" @click="doSuggestRange" style="height: 32px">
              自动建议区间
            </el-button>
          </el-form-item>
          <el-form-item label="Grid Spacing %" :model-value="undefined">
            <el-input-number v-model="spacingPct" :min="0.1" :max="20" :step="0.1" :precision="2" style="width: 130px" />
          </el-form-item>
          <el-form-item label="USDT / Grid">
            <el-input-number v-model="form.usdt_per_grid" :min="1" :step="50" style="width: 130px" />
          </el-form-item>
          <el-form-item label="Stop-Loss Below %">
            <el-input-number v-model="stopLossPct" :min="0" :max="50" :step="0.5" :precision="1" style="width: 140px" />
          </el-form-item>
          <el-form-item label="Pause Buy Above Upper">
            <el-switch v-model="form.pause_buy_above_upper" />
          </el-form-item>
        </el-form>
        <div v-if="suggestedCurrentPrice" class="suggest-hint">
          最近{{ suggestedDays }}天区间：{{ formatPrice(suggestedLower) }} ~ {{ formatPrice(suggestedUpper) }}
          （当前价 {{ formatPrice(suggestedCurrentPrice) }}）
        </div>
        <div v-if="gridLevelCount !== null" class="suggest-hint">
          按当前间距，区间内将生成约 <b>{{ gridLevelCount }}</b> 个格子
        </div>
      </div>

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
          @click="doGridBacktest"
          size="small"
          style="background: #f0b90b; border-color: #f0b90b; color: #000; margin-left: 16px; height: 32px; font-weight: 600"
        >
          Run Grid Backtest
        </el-button>
      </div>
    </el-card>

    <!-- Results -->
    <template v-if="result">
      <el-alert
        v-if="result.stopped_out"
        type="warning"
        show-icon
        :closable="false"
        title="网格已触发止损并停止运行"
        :description="result.stop_reason"
        style="margin-bottom: 16px"
      />

      <!-- Metrics Summary -->
      <el-row :gutter="16" style="margin-bottom: 16px">
        <el-col :span="4">
          <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
            <div class="metric-card">
              <div class="metric-label">Total Return</div>
              <div class="metric-value" :style="{ color: result.metrics.total_return_pct >= 0 ? '#67C23A' : '#F56C6C' }">
                {{ result.metrics.total_return_pct >= 0 ? '+' : '' }}{{ result.metrics.total_return_pct.toFixed(2) }}%
              </div>
              <div class="metric-sub">Final Equity ${{ result.metrics.final_equity.toFixed(2) }}</div>
            </div>
          </el-card>
        </el-col>
        <el-col :span="4">
          <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
            <div class="metric-card">
              <div class="metric-label">Total Trades</div>
              <div class="metric-value" style="color: #e0e0e0">{{ result.trades?.length || 0 }}</div>
              <div class="metric-sub">{{ tradeSideCounts.buy }} buy · {{ tradeSideCounts.sell }} sell{{ tradeSideCounts.forceClose ? ` · ${tradeSideCounts.forceClose} force-close` : '' }}</div>
            </div>
          </el-card>
        </el-col>
        <el-col :span="4">
          <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
            <div class="metric-card">
              <div class="metric-label">Realized PnL</div>
              <div class="metric-value" :style="{ color: result.metrics.realized_pnl >= 0 ? '#67C23A' : '#F56C6C' }">
                {{ result.metrics.realized_pnl >= 0 ? '+' : '' }}{{ result.metrics.realized_pnl.toFixed(2) }} USDT
              </div>
              <div class="metric-sub">{{ result.metrics.completed_round_trips }} round trips · fees ${{ result.metrics.total_fees.toFixed(2) }}</div>
            </div>
          </el-card>
        </el-col>
        <el-col :span="4">
          <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
            <div class="metric-card">
              <div class="metric-label">Max Drawdown</div>
              <div class="metric-value" style="color: #F56C6C">-{{ result.metrics.max_drawdown_pct.toFixed(2) }}%</div>
              <div class="metric-sub">Max concurrent holdings: {{ result.metrics.max_concurrent_holdings }}</div>
            </div>
          </el-card>
        </el-col>
        <el-col :span="4">
          <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
            <div class="metric-card">
              <div class="metric-label">Remaining Position</div>
              <div class="metric-value" style="color: #f0b90b">{{ result.metrics.remaining_qty.toFixed(6) }}</div>
              <div class="metric-sub">≈ ${{ result.metrics.remaining_value.toFixed(2) }}</div>
            </div>
          </el-card>
        </el-col>
      </el-row>

      <el-card shadow="never" style="background: #1d1e1f; border-color: #333; margin-bottom: 16px">
        <el-tabs v-model="activeChartTab" class="bt-chart-tabs">
          <el-tab-pane label="Grid Kline" name="kline">
            <div ref="klineChartContainer" style="width: 100%; height: 420px"></div>
          </el-tab-pane>
          <el-tab-pane label="Equity Curve" name="equity">
            <div ref="equityChartContainer" style="width: 100%; height: 350px"></div>
          </el-tab-pane>
        </el-tabs>
      </el-card>

      <!-- Trade List -->
      <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
        <template #header>
          <span style="color: #e0e0e0">Trade History ({{ result.trades?.length || 0 }})</span>
        </template>
        <el-table
          :data="result.trades"
          style="width: 100%"
          size="small"
          max-height="500"
          :header-cell-style="{ background: '#252526', color: '#b0b0b0' }"
          :cell-style="{ background: '#1d1e1f', color: '#e0e0e0' }"
        >
          <el-table-column label="Time" width="160">
            <template #default="{ row }">{{ formatTime(row.timestamp) }}</template>
          </el-table-column>
          <el-table-column prop="side" label="Side" width="100">
            <template #default="{ row }">
              <el-tag :type="sideTagType(row.side)" size="small">{{ row.side }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="Price" width="100">
            <template #default="{ row }">{{ formatPrice(row.price) }}</template>
          </el-table-column>
          <el-table-column label="Qty" width="110">
            <template #default="{ row }">{{ row.quantity.toFixed(6) }}</template>
          </el-table-column>
          <el-table-column label="Slot" width="180">
            <template #default="{ row }">{{ formatPrice(row.slot_low) }} ~ {{ formatPrice(row.slot_high) }}</template>
          </el-table-column>
          <el-table-column label="Fee (U)" width="80">
            <template #default="{ row }">{{ row.fee.toFixed(2) }}</template>
          </el-table-column>
          <el-table-column label="PnL (U)" width="100">
            <template #default="{ row }">
              <span v-if="row.side !== 'BUY'" :style="{ color: row.pnl >= 0 ? '#67C23A' : '#F56C6C' }">
                {{ row.pnl >= 0 ? '+' : '' }}{{ row.pnl.toFixed(2) }}
              </span>
              <span v-else style="color: #888">-</span>
            </template>
          </el-table-column>
        </el-table>
      </el-card>
    </template>

    <!-- Empty State -->
    <el-card v-else-if="!loading" shadow="never" style="background: #1d1e1f; border-color: #333; text-align: center; padding: 60px 0">
      <el-empty description="配置网格参数并点击 'Run Grid Backtest' 开始回测" />
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onBeforeUnmount, nextTick, watch } from 'vue'
import { createChart, type IChartApi, type ISeriesApi, type IPriceLine, ColorType } from 'lightweight-charts'
import { runGridBacktest, suggestGridRange, type GridBacktestRequest, type GridResult, type GridTradeSide } from '@/api/grid'
import { fetchKlines } from '@/api/klines'
import { SYMBOLS } from '@/utils/constants'
import { formatPrice, formatTime } from '@/utils/format'
import { ElMessage } from 'element-plus'

const dayPresets = [
  { label: '3D', days: 3 },
  { label: '7D', days: 7 },
  { label: '14D', days: 14 },
  { label: '30D', days: 30 },
  { label: '90D', days: 90 },
  { label: '180D', days: 180 },
]

const form = ref<GridBacktestRequest>({
  symbol: 'BTCUSDT',
  interval: '1m',
  cash: 10000,
  lower_price: 0,
  upper_price: 0,
  grid_spacing_pct: 0.01,
  usdt_per_grid: 200,
  stop_loss_below_pct: 0.05,
  pause_buy_above_upper: true,
})

// UI shows spacing/stop-loss as plain percentages; the API wants fractions.
const spacingPct = computed({
  get: () => Math.round(form.value.grid_spacing_pct * 10000) / 100,
  set: (v: number) => { form.value.grid_spacing_pct = v / 100 },
})
const stopLossPct = computed({
  get: () => Math.round((form.value.stop_loss_below_pct || 0) * 10000) / 100,
  set: (v: number) => { form.value.stop_loss_below_pct = v / 100 },
})
const feePct = ref(0.1)

const activeDays = ref(14)
const useCustomRange = ref(false)
const dateRange = ref<[string, string] | null>(null)

const loading = ref(false)
const suggesting = ref(false)
const suggestedLower = ref(0)
const suggestedUpper = ref(0)
const suggestedCurrentPrice = ref(0)
const suggestedDays = ref(30)

const result = ref<GridResult | null>(null)

const tradeSideCounts = computed(() => {
  const counts = { buy: 0, sell: 0, forceClose: 0 }
  for (const t of result.value?.trades || []) {
    if (t.side === 'BUY') counts.buy++
    else if (t.side === 'SELL') counts.sell++
    else if (t.side === 'FORCE_CLOSE') counts.forceClose++
  }
  return counts
})

const gridLevelCount = computed(() => {
  const { lower_price, upper_price } = form.value
  if (!lower_price || !upper_price || upper_price <= lower_price || spacingPct.value <= 0) return null
  const spacing = spacingPct.value / 100
  return Math.max(1, Math.round(Math.log(upper_price / lower_price) / Math.log(1 + spacing)))
})

function selectPreset(days: number) {
  activeDays.value = days
  useCustomRange.value = false
  dateRange.value = null
}

function disableFutureDate(date: Date): boolean {
  return date.getTime() > Date.now()
}

function sideTagType(side: GridTradeSide): 'success' | 'danger' | 'warning' {
  if (side === 'BUY') return 'success'
  if (side === 'FORCE_CLOSE') return 'warning'
  return 'danger'
}

async function doSuggestRange() {
  suggesting.value = true
  try {
    const res = await suggestGridRange(form.value.symbol, 30)
    suggestedLower.value = res.lower_price
    suggestedUpper.value = res.upper_price
    suggestedCurrentPrice.value = res.current_price
    suggestedDays.value = res.days
    form.value.lower_price = res.lower_price
    form.value.upper_price = res.upper_price
  } catch (e: any) {
    ElMessage.error('获取建议区间失败: ' + (e.response?.data?.message || e.message))
  } finally {
    suggesting.value = false
  }
}

async function doGridBacktest() {
  if (!form.value.lower_price || !form.value.upper_price || form.value.upper_price <= form.value.lower_price) {
    ElMessage.warning('请先设置有效的 Lower/Upper Price（可点击"自动建议区间"）')
    return
  }

  loading.value = true
  result.value = null
  try {
    const req: GridBacktestRequest = {
      ...form.value,
      fee: feePct.value / 100,
    }
    if (useCustomRange.value && dateRange.value) {
      req.start = dateRange.value[0]
      req.end = dateRange.value[1]
    } else {
      req.days = activeDays.value
    }

    const res = await runGridBacktest(req)
    result.value = res
    await nextTick()
    await renderActiveChart()
    ElMessage.success(`Grid backtest complete: ${res.metrics.completed_round_trips} round trips, ${res.metrics.total_return_pct.toFixed(2)}% return`)
  } catch (e: any) {
    ElMessage.error('Grid backtest failed: ' + (e.response?.data?.message || e.message))
  } finally {
    loading.value = false
  }
}

// --- Charts ---
const activeChartTab = ref<'kline' | 'equity'>('kline')
const equityChartContainer = ref<HTMLElement | null>(null)
const klineChartContainer = ref<HTMLElement | null>(null)
let equityChart: IChartApi | null = null
let klineChart: IChartApi | null = null
let equitySeries: ISeriesApi<'Area'> | null = null
let klineCandleSeries: ISeriesApi<'Candlestick'> | null = null
let gridPriceLines: IPriceLine[] = []
let equityResizeObserver: ResizeObserver | null = null
let klineResizeObserver: ResizeObserver | null = null

const tzOffsetSec = -new Date().getTimezoneOffset() * 60

function toLocalChartTime(isoOrTs: string | number): number {
  const utcSec = Math.floor(new Date(isoOrTs).getTime() / 1000)
  return utcSec + tzOffsetSec
}

function intervalSeconds(interval: string): number {
  switch (interval) {
    case '1m': return 60
    case '5m': return 300
    default: return 60
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
  gridPriceLines = []
}

async function renderActiveChart() {
  if (!result.value) return
  if (activeChartTab.value === 'kline') {
    await renderGridKlineChart()
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
    layout: { background: { type: ColorType.Solid, color: '#1d1e1f' }, textColor: '#b0b0b0' },
    grid: { vertLines: { color: '#2a2a2a' }, horzLines: { color: '#2a2a2a' } },
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

  equitySeries.setData(result.value.equity_curve.map(p => ({ time: toLocalChartTime(p.time) as any, value: p.equity })))
  equityChart.timeScale().fitContent()

  equityResizeObserver = new ResizeObserver(() => {
    if (equityChart && equityChartContainer.value) {
      equityChart.applyOptions({ width: equityChartContainer.value.clientWidth })
    }
  })
  equityResizeObserver.observe(equityChartContainer.value)
}

async function renderGridKlineChart() {
  if (!klineChartContainer.value || !result.value) return
  cleanupKlineChart()

  klineChart = createChart(klineChartContainer.value, {
    width: klineChartContainer.value.clientWidth,
    height: 420,
    layout: { background: { type: ColorType.Solid, color: '#1d1e1f' }, textColor: '#b0b0b0' },
    grid: { vertLines: { color: '#2a2a2a' }, horzLines: { color: '#2a2a2a' } },
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

  klineCandleSeries.setData(klines.map(k => ({
    time: toLocalChartTime(k.open_time) as any,
    open: k.open, high: k.high, low: k.low, close: k.close,
  })))

  // Faint horizontal reference lines for every grid level.
  gridPriceLines = result.value.grid_levels.map((price, idx) => {
    const isBoundary = idx === 0 || idx === result.value!.grid_levels.length - 1
    return klineCandleSeries!.createPriceLine({
      price,
      color: isBoundary ? '#f0b90b' : 'rgba(176, 176, 176, 0.35)',
      lineWidth: isBoundary ? 2 : 1,
      lineStyle: isBoundary ? 0 : 2, // solid for range boundary, dashed for inner levels
      axisLabelVisible: isBoundary,
      title: isBoundary ? (idx === 0 ? 'Lower' : 'Upper') : '',
    })
  })

  const markers = result.value.trades.map((t, idx) => {
    if (t.side === 'BUY') {
      return { id: `t-${idx}`, time: toLocalChartTime(t.timestamp) as any, position: 'belowBar' as const, color: '#67C23A', shape: 'arrowUp' as const, text: 'B' }
    }
    if (t.side === 'SELL') {
      return { id: `t-${idx}`, time: toLocalChartTime(t.timestamp) as any, position: 'aboveBar' as const, color: '#F56C6C', shape: 'arrowDown' as const, text: 'S' }
    }
    return { id: `t-${idx}`, time: toLocalChartTime(t.timestamp) as any, position: 'aboveBar' as const, color: '#e6a23c', shape: 'circle' as const, text: 'X' }
  })
  klineCandleSeries.setMarkers(markers)

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
.grid-params-row {
  margin-top: 8px;
  padding-top: 12px;
  border-top: 1px solid #333;
}
.suggest-hint {
  color: #909399;
  font-size: 12px;
  margin-top: -8px;
  margin-bottom: 8px;
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
</style>
