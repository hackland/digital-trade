<template>
  <el-drawer
    v-model="visible"
    :title="`持仓分析 · ${symbol}`"
    direction="rtl"
    size="580px"
    :destroy-on-close="true"
    style="background: #1d1e1f"
  >
    <div v-if="loading" style="display:flex;align-items:center;justify-content:center;height:300px;color:#888">
      <el-icon class="is-loading" style="margin-right:8px"><Loading /></el-icon>分析中...
    </div>

    <div v-else-if="analysis" style="display:flex;flex-direction:column;gap:16px">

      <!-- 综合建议 -->
      <div :style="summaryStyle" style="padding:14px 16px;border-radius:8px;border-left:4px solid">
        <div style="font-size:15px;font-weight:600;margin-bottom:4px">
          {{ recommendLabel }}
        </div>
        <div style="font-size:13px;opacity:.85">{{ analysis.reason_summary }}</div>
      </div>

      <!-- 价格概览 -->
      <el-card shadow="never" style="background:#252526;border-color:#333">
        <div style="display:grid;grid-template-columns:repeat(3,1fr);gap:12px;font-size:13px">
          <div>
            <div style="color:#888;margin-bottom:2px">入场价</div>
            <div style="color:#f0b90b;font-weight:600">{{ fmt(analysis.entry_price) }}</div>
          </div>
          <div>
            <div style="color:#888;margin-bottom:2px">当前价</div>
            <div style="color:#e0e0e0;font-weight:600">{{ fmt(analysis.current_price) }}</div>
          </div>
          <div>
            <div style="color:#888;margin-bottom:2px">浮动盈亏</div>
            <div :style="{color: analysis.unrealized_pnl >= 0 ? '#67C23A' : '#F56C6C', fontWeight:600}">
              {{ fmtPnl(analysis.unrealized_pnl) }}
              <span style="font-size:11px;opacity:.8">({{ analysis.unrealized_pct.toFixed(2) }}%)</span>
            </div>
          </div>
          <div>
            <div style="color:#888;margin-bottom:2px">ATR 止损</div>
            <div style="color:#F56C6C">{{ fmt(analysis.stop_price) }}</div>
          </div>
          <div>
            <div style="color:#888;margin-bottom:2px">日线 EMA50</div>
            <div style="color:#e6a23c">{{ fmt(analysis.daily_ema50) }}</div>
          </div>
          <div>
            <div style="color:#888;margin-bottom:2px">日线 EMA200</div>
            <div style="color:#909399">{{ fmt(analysis.daily_ema200) }}</div>
          </div>
        </div>
      </el-card>

      <!-- K 线图 + 价格线 -->
      <el-card shadow="never" style="background:#1d1e1f;border-color:#333">
        <template #header>
          <div style="display:flex;justify-content:space-between;align-items:center;color:#e0e0e0">
            <span>价格走势</span>
            <el-radio-group v-model="chartInterval" size="small" @change="reloadChart">
              <el-radio-button value="1h">1h</el-radio-button>
              <el-radio-button value="4h">4h</el-radio-button>
              <el-radio-button value="1d">1d</el-radio-button>
            </el-radio-group>
          </div>
        </template>
        <div ref="chartContainer" style="width:100%;height:320px" />
      </el-card>

      <!-- 风险维度 -->
      <el-card shadow="never" style="background:#252526;border-color:#333">
        <template #header><span style="color:#e0e0e0">风险维度</span></template>
        <div style="display:flex;flex-direction:column;gap:8px">
          <div
            v-for="dim in analysis.dimensions"
            :key="dim.name"
            style="display:flex;align-items:flex-start;gap:10px;padding:8px 10px;border-radius:6px;background:#1d1e1f"
          >
            <div style="width:8px;height:8px;border-radius:50%;margin-top:5px;flex-shrink:0"
              :style="{background: statusColor(dim.status)}" />
            <div style="flex:1;min-width:0">
              <div style="display:flex;justify-content:space-between;font-size:13px">
                <span style="color:#e0e0e0;font-weight:500">{{ dim.name }}</span>
                <span :style="{color: statusColor(dim.status)}">{{ dim.value }}</span>
              </div>
              <div style="font-size:12px;color:#888;margin-top:2px">{{ dim.detail }}</div>
            </div>
          </div>
        </div>
      </el-card>

      <!-- 策略评分 -->
      <el-card shadow="never" style="background:#252526;border-color:#333">
        <template #header><span style="color:#e0e0e0">策略状态</span></template>
        <div style="display:grid;grid-template-columns:repeat(2,1fr);gap:12px;font-size:13px">
          <div>
            <div style="color:#888;margin-bottom:2px">综合评分</div>
            <div :style="{color: analysis.composite_score >= 0 ? '#67C23A' : '#F56C6C', fontWeight:600}">
              {{ analysis.composite_score.toFixed(4) }}
            </div>
          </div>
          <div>
            <div style="color:#888;margin-bottom:2px">卖出阈值</div>
            <div style="color:#e0e0e0">{{ analysis.sell_threshold.toFixed(4) }}</div>
          </div>
          <div>
            <div style="color:#888;margin-bottom:2px">距阈值空间</div>
            <div :style="{color: analysis.score_margin > 0 ? '#67C23A' : '#F56C6C'}">
              {{ analysis.score_margin.toFixed(4) }}
            </div>
          </div>
          <div>
            <div style="color:#888;margin-bottom:2px">持仓K线数</div>
            <div style="color:#e0e0e0">{{ analysis.bars_since_entry }}</div>
          </div>
          <div v-if="analysis.hold_reason" style="grid-column:span 2">
            <div style="color:#888;margin-bottom:2px">持仓原因</div>
            <div style="color:#b0b0b0;font-size:12px">{{ analysis.hold_reason }}</div>
          </div>
        </div>
      </el-card>

      <div style="font-size:11px;color:#555;text-align:right">
        分析基于当前市价与策略诊断，每次打开实时刷新
      </div>
    </div>

    <div v-else-if="error" style="display:flex;align-items:center;justify-content:center;height:200px;color:#888">
      {{ error }}
    </div>
  </el-drawer>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick, onUnmounted } from 'vue'
import { Loading } from '@element-plus/icons-vue'
import { createChart, ColorType, type IChartApi, type ISeriesApi, type Time } from 'lightweight-charts'
import { get } from '@/api/http'
import { fetchKlines } from '@/api/klines'

interface RiskDimension { name: string; status: string; value: string; detail: string }
interface PositionAnalysis {
  symbol: string
  entry_price: number; current_price: number; quantity: number
  unrealized_pnl: number; unrealized_pct: number
  stop_price: number; daily_ema50: number; daily_ema200: number
  dist_to_stop_pct: number; dist_to_ema200_pct: number
  composite_score: number; sell_threshold: number; score_margin: number
  bars_since_entry: number; hold_reason: string
  regime: string; regime_label: string
  htf_bullish: boolean; htf_blocked: boolean
  dimensions: RiskDimension[]
  risk_level: string; recommendation: string; reason_summary: string
}

const props = defineProps<{ symbol: string }>()
const visible = defineModel<boolean>({ default: false })

const loading = ref(false)
const error = ref('')
const analysis = ref<PositionAnalysis | null>(null)
const chartInterval = ref('4h')
const chartContainer = ref<HTMLElement | null>(null)

let chart: IChartApi | null = null
let candleSeries: ISeriesApi<'Candlestick'> | null = null
let resizeObs: ResizeObserver | null = null

const tzOffset = -new Date().getTimezoneOffset() * 60
function toT(iso: string): Time { return (Math.floor(new Date(iso).getTime() / 1000) + tzOffset) as Time }
function fmt(v: number) { return v ? v.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 }) : '--' }
function fmtPnl(v: number) { return (v >= 0 ? '+' : '') + v.toFixed(2) + ' USDT' }

function statusColor(s: string) {
  return s === 'ok' ? '#67C23A' : s === 'warning' ? '#e6a23c' : '#F56C6C'
}

const recommendLabel = computed(() => {
  const map: Record<string, string> = {
    hold: '✅ 继续持有',
    watch: '⚠️ 关注风险',
    consider_close: '🔶 建议考虑平仓',
    close_now: '🔴 建议立即平仓',
  }
  return map[analysis.value?.recommendation ?? ''] ?? '--'
})

const summaryStyle = computed(() => {
  const level = analysis.value?.risk_level
  const colors: Record<string, { bg: string; border: string }> = {
    low:      { bg: '#1a2a1a', border: '#67C23A' },
    medium:   { bg: '#2a2510', border: '#e6a23c' },
    high:     { bg: '#2a1a10', border: '#f56c6c' },
    critical: { bg: '#3a1010', border: '#ff0000' },
  }
  const c = colors[level ?? 'low']
  return { background: c.bg, borderColor: c.border, color: '#e0e0e0' }
})

async function load() {
  if (!props.symbol) return
  loading.value = true
  error.value = ''
  analysis.value = null
  try {
    analysis.value = await get<PositionAnalysis>(`/positions/${props.symbol}/analysis`)
    await nextTick()
    await buildChart()
  } catch (e: any) {
    error.value = e?.response?.data?.message || e?.message || '加载失败'
  } finally {
    loading.value = false
  }
}

async function buildChart() {
  if (!chartContainer.value || !analysis.value) return
  destroyChart()

  chart = createChart(chartContainer.value, {
    width: chartContainer.value.clientWidth,
    height: 320,
    layout: { background: { type: ColorType.Solid, color: '#1d1e1f' }, textColor: '#b0b0b0' },
    grid: { vertLines: { color: '#2a2a2a' }, horzLines: { color: '#2a2a2a' } },
    crosshair: { mode: 0 },
    timeScale: { timeVisible: true, secondsVisible: false },
    rightPriceScale: { borderColor: '#333' },
  })

  candleSeries = chart.addCandlestickSeries({
    upColor: '#67C23A', downColor: '#F56C6C',
    borderUpColor: '#67C23A', borderDownColor: '#F56C6C',
    wickUpColor: '#67C23A', wickDownColor: '#F56C6C',
  })
  candleSeries.priceScale().applyOptions({ scaleMargins: { top: 0.05, bottom: 0.05 } })

  const klines = await fetchKlines({ symbol: props.symbol, interval: chartInterval.value, limit: 200 })
  if (klines?.length) {
    candleSeries.setData(klines.map(k => ({
      time: toT(k.open_time), open: k.open, high: k.high, low: k.low, close: k.close,
    })))
  }

  const a = analysis.value
  // Entry price line
  if (a.entry_price) {
    candleSeries.createPriceLine({ price: a.entry_price, color: '#f0b90b', lineWidth: 2, lineStyle: 0, title: `入场 ${fmt(a.entry_price)}` })
  }
  // ATR stop line
  if (a.stop_price) {
    candleSeries.createPriceLine({ price: a.stop_price, color: '#F56C6C', lineWidth: 1, lineStyle: 2, title: `止损 ${fmt(a.stop_price)}` })
  }
  // EMA50 line
  if (a.daily_ema50) {
    candleSeries.createPriceLine({ price: a.daily_ema50, color: '#e6a23c', lineWidth: 1, lineStyle: 1, title: `EMA50 ${fmt(a.daily_ema50)}` })
  }
  // EMA200 line
  if (a.daily_ema200) {
    candleSeries.createPriceLine({ price: a.daily_ema200, color: '#909399', lineWidth: 1, lineStyle: 1, title: `EMA200 ${fmt(a.daily_ema200)}` })
  }

  chart.timeScale().fitContent()

  resizeObs = new ResizeObserver(() => {
    if (chart && chartContainer.value)
      chart.applyOptions({ width: chartContainer.value.clientWidth })
  })
  resizeObs.observe(chartContainer.value)
}

async function reloadChart() {
  if (!analysis.value) return
  await nextTick()
  await buildChart()
}

function destroyChart() {
  resizeObs?.disconnect(); resizeObs = null
  chart?.remove(); chart = null; candleSeries = null
}

watch(visible, (v) => { if (v) load() })
onUnmounted(destroyChart)
</script>
