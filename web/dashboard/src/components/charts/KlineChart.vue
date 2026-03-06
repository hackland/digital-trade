<template>
  <div ref="chartContainer" style="width: 100%; height: 500px"></div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch } from 'vue'
import { createChart, type IChartApi, type ISeriesApi, ColorType } from 'lightweight-charts'
import { fetchKlines } from '@/api/klines'
import { useWebSocket } from '@/composables/useWebSocket'

const props = defineProps<{
  symbol: string
  interval: string
}>()

const chartContainer = ref<HTMLElement | null>(null)
let chart: IChartApi | null = null
let candleSeries: ISeriesApi<'Candlestick'> | null = null
let volumeSeries: ISeriesApi<'Histogram'> | null = null
let unsub: (() => void) | null = null

const tzOffsetSec = -new Date().getTimezoneOffset() * 60

function toLocalChartTime(isoOrTs: string | number): number {
  const utcSec = Math.floor(new Date(isoOrTs).getTime() / 1000)
  return utcSec + tzOffsetSec
}

function volumeColor(open: number, close: number): string {
  return close >= open ? 'rgba(103,194,58,0.4)' : 'rgba(245,108,108,0.4)'
}

async function loadChart() {
  if (!chartContainer.value) return

  chart?.remove()

  chart = createChart(chartContainer.value, {
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
  })

  // Candlestick series — top 70%
  candleSeries = chart.addCandlestickSeries({
    upColor: '#67C23A',
    downColor: '#F56C6C',
    borderUpColor: '#67C23A',
    borderDownColor: '#F56C6C',
    wickUpColor: '#67C23A',
    wickDownColor: '#F56C6C',
    priceScaleId: 'right',
  })
  candleSeries.priceScale().applyOptions({
    scaleMargins: { top: 0.05, bottom: 0.3 },
  })

  // Volume histogram — bottom 25%
  volumeSeries = chart.addHistogramSeries({
    priceFormat: { type: 'volume' },
    priceScaleId: 'volume',
  })
  volumeSeries.priceScale().applyOptions({
    scaleMargins: { top: 0.75, bottom: 0 },
  })

  // Load historical data
  try {
    const klines = await fetchKlines({ symbol: props.symbol, interval: props.interval, limit: 500 })
    if (klines && klines.length > 0) {
      const candleData: any[] = []
      const volData: any[] = []

      for (const k of klines) {
        const t = toLocalChartTime(k.open_time || (k as any).OpenTime)
        const o = k.open ?? (k as any).Open
        const c = k.close ?? (k as any).Close
        const h = k.high ?? (k as any).High
        const l = k.low ?? (k as any).Low
        const v = k.volume ?? (k as any).Volume

        candleData.push({ time: t as any, open: o, high: h, low: l, close: c })
        volData.push({ time: t as any, value: v, color: volumeColor(o, c) })
      }

      candleSeries.setData(candleData)
      volumeSeries.setData(volData)
    }
  } catch { /* backend may not be ready */ }

  chart.timeScale().fitContent()

  // Real-time updates
  const ws = useWebSocket()
  unsub?.()
  unsub = ws.subscribe(`kline:${props.symbol}:${props.interval}`, (data: any) => {
    const kline = data.kline ?? data.Kline ?? data
    if (!kline) return

    const t = toLocalChartTime(kline.open_time ?? kline.OpenTime)
    const o = kline.open ?? kline.Open
    const h = kline.high ?? kline.High
    const l = kline.low ?? kline.Low
    const c = kline.close ?? kline.Close
    const v = kline.volume ?? kline.Volume

    candleSeries?.update({ time: t as any, open: o, high: h, low: l, close: c })
    volumeSeries?.update({ time: t as any, value: v, color: volumeColor(o, c) })
  })
}

onMounted(loadChart)

watch(() => [props.symbol, props.interval], loadChart)

onUnmounted(() => {
  unsub?.()
  chart?.remove()
})
</script>
