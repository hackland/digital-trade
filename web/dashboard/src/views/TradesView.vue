<template>
  <div style="display: flex; flex-direction: column; gap: 16px">
    <!-- Chart Card -->
    <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
      <template #header>
        <div style="display: flex; justify-content: space-between; align-items: center; color: #e0e0e0">
          <span>成交图表</span>
          <div style="display: flex; gap: 8px; align-items: center">
            <el-select v-model="chartSymbol" size="small" style="width: 130px">
              <el-option v-for="s in SYMBOLS" :key="s" :label="s" :value="s" />
            </el-select>
            <el-select v-model="chartInterval" size="small" style="width: 90px">
              <el-option v-for="iv in INTERVALS" :key="iv" :label="iv" :value="iv" />
            </el-select>
            <el-button size="small" type="primary" @click="loadChartData">加载</el-button>
            <el-button size="small" @click="clearSelection">取消选中</el-button>
            <span style="font-size: 12px; color: #888">
              共 {{ chartTrades.length }} 个标记
            </span>
          </div>
        </div>
      </template>

      <!-- 容器始终在 DOM 中，保持尺寸；遮罩层覆盖在上方 -->
      <div style="position: relative; width: 100%; height: 400px">
        <div ref="chartContainer" style="width: 100%; height: 100%" />
        <div v-if="chartLoading" style="position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; background: #1d1e1f; color: #888; z-index: 10">
          <el-icon class="is-loading" style="margin-right: 8px"><Loading /></el-icon>
          加载中...
        </div>
        <div v-else-if="!chartReady" style="position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; background: #1d1e1f; color: #888; z-index: 10">
          请选择交易对和周期后点击「加载」
        </div>
      </div>

      <!-- 选中成交详情 -->
      <div v-if="selectedTrade" style="margin-top: 12px; padding: 10px 14px; background: #252526; border-radius: 6px; display: flex; gap: 32px; font-size: 13px; color: #e0e0e0; flex-wrap: wrap">
        <div>
          <span style="color: #888">方向：</span>
          <el-tag :type="selectedTrade.side === 'BUY' ? 'success' : 'danger'" size="small">{{ selectedTrade.side }}</el-tag>
        </div>
        <div><span style="color: #888">价格：</span><span style="color: #f0b90b">{{ formatPrice(selectedTrade.price) }}</span></div>
        <div><span style="color: #888">数量：</span>{{ formatNumber(selectedTrade.quantity, 6) }}</div>
        <div><span style="color: #888">手续费：</span>{{ formatNumber(selectedTrade.fee, 6) }} {{ selectedTrade.fee_asset }}</div>
        <div>
          <span style="color: #888">PnL：</span>
          <span :style="{ color: selectedTrade.realized_pnl > 0 ? '#67C23A' : selectedTrade.realized_pnl < 0 ? '#F56C6C' : '#888' }">
            {{ formatPnl(selectedTrade.realized_pnl) }}
          </span>
        </div>
        <div><span style="color: #888">策略：</span>{{ selectedTrade.strategy_name || '-' }}</div>
        <div><span style="color: #888">时间：</span>{{ formatTime(selectedTrade.timestamp) }}</div>
      </div>
    </el-card>

    <!-- Trade Table Card -->
    <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
      <template #header>
        <div style="display: flex; justify-content: space-between; align-items: center; color: #e0e0e0">
          <span>成交记录</span>
          <div style="display: flex; gap: 8px">
            <el-select v-model="filter.symbol" placeholder="Symbol" clearable size="small" style="width: 140px">
              <el-option v-for="s in SYMBOLS" :key="s" :label="s" :value="s" />
            </el-select>
            <el-button size="small" @click="loadTableData">查询</el-button>
          </div>
        </div>
      </template>

      <el-table
        ref="tableRef"
        :data="trades"
        style="width: 100%"
        size="small"
        highlight-current-row
        :current-row-key="selectedTrade?.id"
        row-key="id"
        :header-cell-style="{ background: '#252526', color: '#b0b0b0' }"
        :row-class-name="tableRowClass"
        @row-click="onTableRowClick"
      >
        <el-table-column prop="symbol" label="Symbol" width="100" />
        <el-table-column prop="side" label="方向" width="70">
          <template #default="{ row }">
            <el-tag :type="row.side === 'BUY' ? 'success' : 'danger'" size="small">{{ row.side }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="price" label="价格">
          <template #default="{ row }">{{ formatPrice(row.price) }}</template>
        </el-table-column>
        <el-table-column prop="quantity" label="数量">
          <template #default="{ row }">{{ formatNumber(row.quantity, 6) }}</template>
        </el-table-column>
        <el-table-column prop="fee" label="手续费" width="140">
          <template #default="{ row }">
            <span v-if="!row.fee || row.fee === 0" style="color:#666">--</span>
            <span v-else style="color:#b0b0b0">
              {{ formatNumber(row.fee, 6) }}
              <span style="color:#888;font-size:11px">{{ row.fee_asset }}</span>
            </span>
          </template>
        </el-table-column>
        <el-table-column prop="realized_pnl" label="PnL">
          <template #default="{ row }">
            <span :style="{ color: row.realized_pnl > 0 ? '#67C23A' : row.realized_pnl < 0 ? '#F56C6C' : '#888' }">
              {{ formatPnl(row.realized_pnl) }}
            </span>
          </template>
        </el-table-column>
        <el-table-column prop="strategy_name" label="策略" />
        <el-table-column prop="timestamp" label="时间" width="160">
          <template #default="{ row }">{{ formatTime(row.timestamp) }}</template>
        </el-table-column>
      </el-table>

      <div style="margin-top: 16px; display: flex; justify-content: center">
        <el-pagination
          :current-page="page"
          :page-size="pageSize"
          :total="total"
          layout="prev, pager, next"
          @current-change="onPageChange"
        />
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, nextTick } from 'vue'
import { ElMessage } from 'element-plus'
import { Loading } from '@element-plus/icons-vue'
import { createChart, ColorType, type IChartApi, type ISeriesApi, type SeriesMarker, type Time } from 'lightweight-charts'
import { fetchTrades } from '@/api/trades'
import { fetchKlines } from '@/api/klines'
import { useWebSocket } from '@/composables/useWebSocket'
import type { TradeRecord } from '@/types/models'
import { SYMBOLS, INTERVALS } from '@/utils/constants'
import { formatNumber, formatPrice, formatPnl, formatTime } from '@/utils/format'

// ── chart state ──────────────────────────────────────────────────────────────
const chartContainer = ref<HTMLElement | null>(null)
const chartReady = ref(false)
const chartLoading = ref(false)
let chart: IChartApi | null = null
let candleSeries: ISeriesApi<'Candlestick'> | null = null
let volumeSeries: ISeriesApi<'Histogram'> | null = null
let resizeObserver: ResizeObserver | null = null
let wsUnsub: (() => void) | null = null

const chartSymbol = ref(SYMBOLS[0])
const chartInterval = ref('1h')
const chartTrades = ref<TradeRecord[]>([])
const selectedTrade = ref<TradeRecord | null>(null)

// ── table state ───────────────────────────────────────────────────────────────
const tableRef = ref()
const trades = ref<TradeRecord[]>([])
const filter = ref({ symbol: '' })
const page = ref(1)
const pageSize = 20
const total = ref(0)

// ── helpers ───────────────────────────────────────────────────────────────────
const tzOffsetSec = -new Date().getTimezoneOffset() * 60

function toLocalChartTime(iso: string): number {
  return Math.floor(new Date(iso).getTime() / 1000) + tzOffsetSec
}

function volumeColor(open: number, close: number): string {
  return close >= open ? 'rgba(103,194,58,0.4)' : 'rgba(245,108,108,0.4)'
}

// ── markers ───────────────────────────────────────────────────────────────────
function buildMarkers(tradeList: TradeRecord[]): SeriesMarker<Time>[] {
  return [...tradeList].sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime()).map((t, idx) => {
    const isBuy = t.side === 'BUY'
    const isSelected = selectedTrade.value?.id === t.id
    return {
      id: String(t.id ?? idx),
      time: toLocalChartTime(t.timestamp) as Time,
      position: isBuy ? 'belowBar' : 'aboveBar',
      color: isSelected
        ? (isBuy ? '#22c55e' : '#ef4444')
        : (isBuy ? '#67C23A' : '#F56C6C'),
      shape: isBuy ? 'arrowUp' : 'arrowDown',
      text: isSelected ? (isBuy ? 'B★' : 'S★') : (isBuy ? 'B' : 'S'),
    } as SeriesMarker<Time>
  })
}

function applyMarkers() {
  if (!candleSeries) return
  candleSeries.setMarkers(buildMarkers(chartTrades.value))
}

// ── chart lifecycle ───────────────────────────────────────────────────────────
function destroyChart() {
  resizeObserver?.disconnect()
  resizeObserver = null
  wsUnsub?.()
  wsUnsub = null
  chart?.remove()
  chart = null
  candleSeries = null
  volumeSeries = null
  chartReady.value = false
}

async function loadChartData() {
  destroyChart()
  chartLoading.value = true

  try {
    // 1. Load trades for this symbol (up to 500 for markers)
    const tradeRes = await fetchTrades({ symbol: chartSymbol.value, limit: 500, offset: 0 })
    chartTrades.value = (tradeRes.data as TradeRecord[]) ?? []

    // 2. Determine K-line time range from trades (add padding)
    let startIso: string | undefined
    let endIso: string | undefined
    if (chartTrades.value.length > 0) {
      const times = chartTrades.value.map(t => new Date(t.timestamp).getTime())
      const earliest = Math.min(...times)
      const latest = Math.max(...times)
      const padMs = intervalPadMs(chartInterval.value) * 50
      startIso = new Date(earliest - padMs).toISOString()
      endIso = new Date(latest + padMs).toISOString()
    }

    // 3. Load K-lines
    const klines = await fetchKlines({
      symbol: chartSymbol.value,
      interval: chartInterval.value,
      limit: 1000,
      ...(startIso ? { start: startIso } : {}),
      ...(endIso ? { end: endIso } : {}),
    })

    if (!klines?.length) {
      ElMessage.warning('未获取到 K 线数据')
      chartLoading.value = false
      return
    }

    // 4. Build chart
    await nextTick()
    if (!chartContainer.value) return

    chart = createChart(chartContainer.value, {
      width: chartContainer.value.clientWidth,
      height: 400,
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

    candleSeries = chart.addCandlestickSeries({
      upColor: '#67C23A',
      downColor: '#F56C6C',
      borderUpColor: '#67C23A',
      borderDownColor: '#F56C6C',
      wickUpColor: '#67C23A',
      wickDownColor: '#F56C6C',
      priceScaleId: 'right',
    })
    candleSeries.priceScale().applyOptions({ scaleMargins: { top: 0.05, bottom: 0.3 } })

    volumeSeries = chart.addHistogramSeries({
      priceFormat: { type: 'volume' },
      priceScaleId: 'volume',
    })
    volumeSeries.priceScale().applyOptions({ scaleMargins: { top: 0.75, bottom: 0 } })

    // 5. Fill data
    candleSeries.setData(klines.map(k => ({
      time: toLocalChartTime(k.open_time) as Time,
      open: k.open, high: k.high, low: k.low, close: k.close,
    })))
    volumeSeries.setData(klines.map(k => ({
      time: toLocalChartTime(k.open_time) as Time,
      value: k.volume,
      color: volumeColor(k.open, k.close),
    })))

    // 6. Apply trade markers
    applyMarkers()
    chart.timeScale().fitContent()

    // 7. ResizeObserver
    resizeObserver = new ResizeObserver(() => {
      if (chart && chartContainer.value) {
        chart.applyOptions({ width: chartContainer.value.clientWidth })
      }
    })
    resizeObserver.observe(chartContainer.value)

    // 8. Real-time trade updates
    const ws = useWebSocket()
    wsUnsub = ws.subscribe('trade', (data: any) => {
      const t: TradeRecord = data
      if (t.symbol !== chartSymbol.value) return
      // Avoid duplicates
      if (chartTrades.value.some(x => x.id === t.id)) return
      chartTrades.value = [...chartTrades.value, t]
      applyMarkers()
      // Also prepend to table if same symbol filter
      if (!filter.value.symbol || filter.value.symbol === t.symbol) {
        trades.value = [t, ...trades.value]
        total.value += 1
      }
    })

    chartReady.value = true
  } catch (e: any) {
    const msg = e?.response?.data?.message || e?.message || String(e)
    ElMessage.error(`图表加载失败: ${msg}`)
    console.error('[TradesView] loadChartData error:', e)
  } finally {
    chartLoading.value = false
  }
}

// Returns approx ms per bar for padding calculation
function intervalPadMs(iv: string): number {
  const map: Record<string, number> = {
    '1m': 60_000, '3m': 180_000, '5m': 300_000, '15m': 900_000,
    '30m': 1_800_000, '1h': 3_600_000, '2h': 7_200_000,
    '4h': 14_400_000, '6h': 21_600_000, '12h': 43_200_000,
    '1d': 86_400_000,
  }
  return map[iv] ?? 3_600_000
}

// ── selection ─────────────────────────────────────────────────────────────────
function onTableRowClick(row: TradeRecord) {
  if (selectedTrade.value?.id === row.id) {
    clearSelection()
    return
  }
  selectedTrade.value = row

  // Sync chart: re-apply markers with highlight, then scroll to the trade time
  applyMarkers()
  if (chart && row.timestamp) {
    const t = toLocalChartTime(row.timestamp) as Time
    chart.timeScale().scrollToPosition(0, false)
    chart.timeScale().setVisibleRange({
      from: (toLocalChartTime(row.timestamp) - intervalPadMs(chartInterval.value) * 30 / 1000) as Time,
      to: (toLocalChartTime(row.timestamp) + intervalPadMs(chartInterval.value) * 30 / 1000) as Time,
    })
    // Set visible logical range centered on the trade
    void t
  }
}

function clearSelection() {
  selectedTrade.value = null
  applyMarkers()
}

function tableRowClass({ row }: { row: TradeRecord }) {
  if (selectedTrade.value?.id === row.id) return 'trade-row-selected'
  return 'trade-row'
}

// ── table data ────────────────────────────────────────────────────────────────
async function loadTableData() {
  try {
    const params: Record<string, any> = { limit: pageSize, offset: (page.value - 1) * pageSize }
    if (filter.value.symbol) params.symbol = filter.value.symbol
    const res = await fetchTrades(params)
    trades.value = res.data as TradeRecord[]
    total.value = res.total
  } catch {}
}

function onPageChange(p: number) {
  page.value = p
  loadTableData()
}

onMounted(() => {
  loadTableData()
  loadChartData()
})
onUnmounted(destroyChart)
</script>

<style scoped>
:deep(.trade-row-selected td) {
  background: #2a2412 !important;
  color: #f0b90b !important;
}
:deep(.trade-row td) {
  background: #1d1e1f;
  color: #e0e0e0;
}
:deep(.el-table__row) {
  cursor: pointer;
}
:deep(.el-table__row:hover td) {
  background: #252526 !important;
}
</style>
