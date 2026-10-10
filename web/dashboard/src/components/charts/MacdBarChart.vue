<template>
  <div>
    <div class="cap">{{ title }}</div>
    <div ref="el" style="width: 100%" :style="{ height: height + 'px' }"></div>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { createChart, ColorType, type IChartApi } from 'lightweight-charts'
import type { Bar } from '@/api/mtfMacd'

const props = withDefaults(defineProps<{
  title: string
  bars: Bar[]
  markTime?: number // unix sec (UTC) of the candle to mark
  markDir?: 'long' | 'short'
  height?: number
}>(), { height: 340 })

const el = ref<HTMLElement | null>(null)
let chart: IChartApi | null = null
const tz = -new Date().getTimezoneOffset() * 60

function render() {
  if (!el.value) return
  chart?.remove()
  chart = createChart(el.value, {
    layout: { background: { type: ColorType.Solid, color: '#1d1e1f' }, textColor: '#b0b0b0' },
    grid: { vertLines: { color: '#2a2a2a' }, horzLines: { color: '#2a2a2a' } },
    timeScale: { timeVisible: true, secondsVisible: false },
  })
  const candle = chart.addCandlestickSeries({
    upColor: '#26a69a', downColor: '#ef5350', borderVisible: false,
    wickUpColor: '#26a69a', wickDownColor: '#ef5350',
  })
  candle.priceScale().applyOptions({ scaleMargins: { top: 0.04, bottom: 0.42 } })
  candle.setData(props.bars.map(b => ({ time: (b.t + tz) as any, open: b.o, high: b.h, low: b.l, close: b.c })))

  const hist = chart.addHistogramSeries({ priceScaleId: 'macd', priceLineVisible: false, lastValueVisible: false })
  hist.setData(props.bars.map(b => ({ time: (b.t + tz) as any, value: b.hist, color: b.hist >= 0 ? 'rgba(38,166,154,.7)' : 'rgba(239,83,80,.7)' })))
  const dif = chart.addLineSeries({ priceScaleId: 'macd', color: '#f0b90b', lineWidth: 1, priceLineVisible: false, lastValueVisible: false })
  dif.setData(props.bars.map(b => ({ time: (b.t + tz) as any, value: b.macd })))
  const dea = chart.addLineSeries({ priceScaleId: 'macd', color: '#4da3ff', lineWidth: 1, priceLineVisible: false, lastValueVisible: false })
  dea.setData(props.bars.map(b => ({ time: (b.t + tz) as any, value: b.signal })))
  chart.priceScale('macd').applyOptions({ scaleMargins: { top: 0.62, bottom: 0.02 } })
  // zero axis for MACD pane
  dif.createPriceLine({ price: 0, color: '#666', lineWidth: 1, lineStyle: 2, axisLabelVisible: false, title: '0' })

  if (props.markTime) {
    const long = props.markDir === 'long'
    candle.setMarkers([{
      time: (props.markTime + tz) as any,
      position: long ? 'belowBar' : 'aboveBar',
      color: long ? '#26a69a' : '#ef5350',
      shape: long ? 'arrowUp' : 'arrowDown',
      text: long ? '做多' : '做空',
    }])
  }
  chart.timeScale().fitContent()
}

onMounted(render)
watch(() => [props.bars, props.markTime], render)
onBeforeUnmount(() => chart?.remove())
</script>

<style scoped>
.cap { color: #b0b0b0; font-size: 12px; margin: 6px 0 2px }
</style>
