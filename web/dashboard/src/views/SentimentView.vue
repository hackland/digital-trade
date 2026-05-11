<template>
  <div style="display:flex;flex-direction:column;gap:16px">

    <!-- 顶部：核心指标的紧凑卡片 -->
    <div style="display:grid;grid-template-columns:repeat(2,1fr);gap:12px">

      <!-- 资金费率 -->
      <el-card shadow="never" :style="cardStyle(funding?.verdict)">
        <div style="display:flex;justify-content:space-between;align-items:center">
          <span style="color:#888;font-size:12px">资金费率 (8h)</span>
          <el-tag size="small" :type="fundingTagType">{{ fundingVerdictLabel }}</el-tag>
        </div>
        <div style="font-size:22px;font-weight:600;margin-top:6px"
          :style="{color: funding ? (funding.current >= 0 ? '#67C23A' : '#F56C6C') : '#888'}">
          {{ funding ? (funding.current * 100).toFixed(4) + '%' : '--' }}
        </div>
        <div style="font-size:11px;color:#888;margin-top:4px">{{ funding?.hint || '加载中...' }}</div>
      </el-card>

      <!-- F&G -->
      <el-card shadow="never" :style="cardStyle(feargreed?.verdict)">
        <div style="display:flex;justify-content:space-between;align-items:center">
          <span style="color:#888;font-size:12px">恐惧贪婪指数</span>
          <el-tag size="small" :type="fgTagType">{{ feargreed?.current_class || '--' }}</el-tag>
        </div>
        <div style="font-size:22px;font-weight:600;margin-top:6px" :style="{color: fgColor}">
          {{ feargreed?.current ?? '--' }} <span style="font-size:12px;color:#888">/ 100</span>
        </div>
        <div style="font-size:11px;color:#888;margin-top:4px">{{ feargreed?.hint || '加载中...' }}</div>
      </el-card>
    </div>

    <!-- 资金费率历史曲线 -->
    <el-card shadow="never" style="background:#1d1e1f;border-color:#333">
      <template #header>
        <div style="display:flex;justify-content:space-between;align-items:center;color:#e0e0e0">
          <span>资金费率 · 近 30 期</span>
          <span style="font-size:12px;color:#888">
            下次结算: {{ funding ? formatTime(funding.next_funding) : '--' }}
          </span>
        </div>
      </template>
      <div ref="fundingChartEl" style="width:100%;height:220px" />
    </el-card>

    <!-- F&G 历史 -->
    <el-card shadow="never" style="background:#1d1e1f;border-color:#333">
      <template #header><span style="color:#e0e0e0">恐惧贪婪指数 · 近 30 天</span></template>
      <div ref="fgChartEl" style="width:100%;height:220px" />
    </el-card>

    <!-- 数据说明 -->
    <el-card shadow="never" style="background:#1d1e1f;border-color:#333">
      <template #header><span style="color:#e0e0e0">关于这些数据</span></template>
      <ul style="color:#b0b0b0;font-size:13px;line-height:1.8;margin:0;padding-left:18px">
        <li><strong style="color:#e0e0e0">资金费率</strong>：来自 Binance 永续合约。极端正费率 (≥0.05%) = 多头过热预警；负费率 = 空方占主导。</li>
        <li><strong style="color:#e0e0e0">恐惧贪婪</strong>：alternative.me 综合波动/动量/社交/调查的日级别情绪。≤20 极度恐惧（逆向买入区），≥80 极度贪婪（警惕顶部）。</li>
      </ul>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, nextTick, watch } from 'vue'
import { createChart, ColorType, type IChartApi } from 'lightweight-charts'
import { get } from '@/api/http'

interface FundingResp {
  symbol: string
  current: number
  mark_price: number
  next_funding: string
  history: { time: string; rate: number }[]
  verdict: string
  hint: string
}
interface FGResp {
  current: number
  current_class: string
  history: { time: string; value: number; value_class: string }[]
  verdict: string
  hint: string
}
const funding = ref<FundingResp | null>(null)
const feargreed = ref<FGResp | null>(null)

const fundingChartEl = ref<HTMLElement | null>(null)
const fgChartEl = ref<HTMLElement | null>(null)
let fundingChart: IChartApi | null = null
let fgChart: IChartApi | null = null
let resizeObs: ResizeObserver | null = null

const tzOffset = -new Date().getTimezoneOffset() * 60

function formatTime(iso: string) {
  return new Date(iso).toLocaleString('zh-CN', { hour: '2-digit', minute: '2-digit', month: '2-digit', day: '2-digit' })
}

const fundingTagType = computed(() => {
  const v = funding.value?.verdict
  return v === 'overheated' ? 'danger' : v === 'panic' ? 'warning' : 'success'
})
const fundingVerdictLabel = computed(() => {
  const map: Record<string, string> = { overheated: '过热', panic: '恐慌', neutral: '正常' }
  return map[funding.value?.verdict ?? ''] ?? '--'
})

const fgTagType = computed(() => {
  const v = feargreed.value?.verdict
  if (v === 'extreme_fear' || v === 'fear') return 'success'
  if (v === 'extreme_greed' || v === 'greed') return 'danger'
  return 'info'
})
const fgColor = computed(() => {
  const v = feargreed.value?.current ?? 50
  if (v <= 20) return '#67C23A'
  if (v <= 40) return '#a0d468'
  if (v <= 60) return '#e0e0e0'
  if (v <= 80) return '#e6a23c'
  return '#F56C6C'
})

function cardStyle(verdict?: string) {
  const map: Record<string, string> = {
    overheated: '#3a1010',
    panic: '#3a2410',
    neutral: '#1d1e1f',
    extreme_fear: '#1a2a1a',
    fear: '#1a2a1a',
    greed: '#3a2410',
    extreme_greed: '#3a1010',
  }
  return {
    background: map[verdict ?? 'neutral'] ?? '#1d1e1f',
    borderColor: '#333',
  }
}

async function loadAll() {
  try {
    const [f, g] = await Promise.all([
      get<FundingResp>('/sentiment/funding?symbol=BTCUSDT').catch(() => null),
      get<FGResp>('/sentiment/feargreed').catch(() => null),
    ])
    funding.value = f
    feargreed.value = g
    await nextTick()
    drawFunding()
    drawFG()
  } catch (e) {
    console.error('sentiment load failed', e)
  }
}

function drawFunding() {
  if (!fundingChartEl.value || !funding.value) return
  fundingChart?.remove()
  fundingChart = createChart(fundingChartEl.value, {
    width: fundingChartEl.value.clientWidth,
    height: 220,
    layout: { background: { type: ColorType.Solid, color: '#1d1e1f' }, textColor: '#b0b0b0' },
    grid: { vertLines: { color: '#2a2a2a' }, horzLines: { color: '#2a2a2a' } },
    timeScale: { timeVisible: true, secondsVisible: false },
    rightPriceScale: { borderColor: '#333' },
  })
  const series = fundingChart.addHistogramSeries({ priceFormat: { type: 'percent' } })
  const sorted = [...funding.value.history].sort((a, b) => new Date(a.time).getTime() - new Date(b.time).getTime())
  series.setData(sorted.map(p => ({
    time: (Math.floor(new Date(p.time).getTime() / 1000) + tzOffset) as any,
    value: p.rate * 100,
    color: p.rate >= 0 ? '#67C23A' : '#F56C6C',
  })))
  // 阈值参考线
  series.createPriceLine({ price: 0.05, color: '#F56C6C', lineWidth: 1, lineStyle: 2, title: '过热 0.05%' })
  series.createPriceLine({ price: -0.03, color: '#e6a23c', lineWidth: 1, lineStyle: 2, title: '恐慌 -0.03%' })
  fundingChart.timeScale().fitContent()
}

function drawFG() {
  if (!fgChartEl.value || !feargreed.value) return
  fgChart?.remove()
  fgChart = createChart(fgChartEl.value, {
    width: fgChartEl.value.clientWidth,
    height: 220,
    layout: { background: { type: ColorType.Solid, color: '#1d1e1f' }, textColor: '#b0b0b0' },
    grid: { vertLines: { color: '#2a2a2a' }, horzLines: { color: '#2a2a2a' } },
    timeScale: { timeVisible: false },
    rightPriceScale: { borderColor: '#333' },
  })
  const series = fgChart.addLineSeries({ color: '#f0b90b', lineWidth: 2 })
  const sorted = [...feargreed.value.history].sort((a, b) => new Date(a.time).getTime() - new Date(b.time).getTime())
  series.setData(sorted.map(p => ({
    time: (Math.floor(new Date(p.time).getTime() / 1000) + tzOffset) as any,
    value: p.value,
  })))
  series.createPriceLine({ price: 20, color: '#67C23A', lineWidth: 1, lineStyle: 2, title: '极度恐惧' })
  series.createPriceLine({ price: 80, color: '#F56C6C', lineWidth: 1, lineStyle: 2, title: '极度贪婪' })
  fgChart.timeScale().fitContent()
}

onMounted(() => {
  loadAll()
  resizeObs = new ResizeObserver(() => {
    if (fundingChart && fundingChartEl.value) fundingChart.applyOptions({ width: fundingChartEl.value.clientWidth })
    if (fgChart && fgChartEl.value) fgChart.applyOptions({ width: fgChartEl.value.clientWidth })
  })
  if (fundingChartEl.value) resizeObs.observe(fundingChartEl.value)
  if (fgChartEl.value) resizeObs.observe(fgChartEl.value)
})

onUnmounted(() => {
  resizeObs?.disconnect()
  fundingChart?.remove()
  fgChart?.remove()
})
</script>
