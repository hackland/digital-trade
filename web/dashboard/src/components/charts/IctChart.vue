<template>
  <div ref="el" :style="{ width: '100%', height: height + 'px' }"></div>
</template>

<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount, watch, nextTick } from 'vue'
import {
  createChart, ColorType,
  type IChartApi, type ISeriesApi, type ISeriesPrimitive, type ISeriesPrimitivePaneView,
  type ISeriesPrimitivePaneRenderer, type SeriesAttachedParameter, type SeriesMarker, type Time,
} from 'lightweight-charts'
import type { IctCandle, IctSetup } from '@/api/ict'

const props = withDefaults(defineProps<{
  candles: IctCandle[]
  setups: IctSetup[]
  height?: number
  focusId?: string
}>(), { height: 460, focusId: '' })

const el = ref<HTMLElement | null>(null)
const tz = -new Date().getTimezoneOffset() * 60
const unix = (s: string) => Math.floor(new Date(s).getTime() / 1000)
// setups carry 15m-resolution times; bars on the chart are 1h, so snap to the hour in UTC first
const barTime = (sec: number) => Math.floor(sec / 3600) * 3600 + tz

const GREEN = '#26a69a'
const RED = '#ef5350'

type Shape =
  | { kind: 'rect'; t1: number; t2: number; top: number; bottom: number; fill: string; stroke: string; width: number }
  | { kind: 'line'; t1: number; t2: number; price: number; color: string; dash: number[]; width: number; label: string }

// Draws zones and trade levels under the candles, in price/time space.
class Overlay implements ISeriesPrimitive<Time> {
  chart: IChartApi | null = null
  series: ISeriesApi<'Candlestick'> | null = null
  shapes: Shape[] = []
  firstT = 0
  lastT = 0
  private request: (() => void) | null = null
  private views: ISeriesPrimitivePaneView[] = [new OverlayView(this)]

  attached(p: SeriesAttachedParameter<Time>) {
    this.chart = p.chart as unknown as IChartApi
    this.series = p.series as unknown as ISeriesApi<'Candlestick'>
    this.request = p.requestUpdate
  }
  detached() {
    this.chart = null
    this.series = null
    this.request = null
  }
  paneViews() { return this.views }
  set(shapes: Shape[], firstT: number, lastT: number) {
    this.shapes = shapes
    this.firstT = firstT
    this.lastT = lastT
    this.request?.()
  }
}

class OverlayView implements ISeriesPrimitivePaneView {
  constructor(private src: Overlay) {}
  zOrder() { return 'bottom' as const }
  renderer(): ISeriesPrimitivePaneRenderer { return new OverlayRenderer(this.src) }
}

class OverlayRenderer implements ISeriesPrimitivePaneRenderer {
  constructor(private src: Overlay) {}
  draw(target: any) {
    const { chart, series, shapes, firstT, lastT } = this.src
    if (!chart || !series || !shapes.length) return
    const ts = chart.timeScale()
    const clampT = (t: number) => Math.min(Math.max(t, firstT), lastT)
    target.useBitmapCoordinateSpace((scope: any) => {
      const ctx: CanvasRenderingContext2D = scope.context
      const hr: number = scope.horizontalPixelRatio
      const vr: number = scope.verticalPixelRatio
      ctx.font = `${11 * vr}px sans-serif`
      ctx.textBaseline = 'bottom'
      for (const s of shapes) {
        const x1 = ts.timeToCoordinate(clampT(s.t1) as Time)
        const x2 = ts.timeToCoordinate(clampT(s.t2) as Time)
        if (x1 == null || x2 == null) continue
        if (s.kind === 'rect') {
          const y1 = series.priceToCoordinate(s.top)
          const y2 = series.priceToCoordinate(s.bottom)
          if (y1 == null || y2 == null) continue
          const x = Math.min(x1, x2) * hr
          const w = Math.max(Math.abs(x2 - x1) * hr, 2)
          const y = Math.min(y1, y2) * vr
          const h = Math.max(Math.abs(y2 - y1) * vr, 1)
          ctx.fillStyle = s.fill
          ctx.fillRect(x, y, w, h)
          ctx.strokeStyle = s.stroke
          ctx.lineWidth = s.width * hr
          ctx.setLineDash([])
          ctx.strokeRect(x, y, w, h)
        } else {
          const y = series.priceToCoordinate(s.price)
          if (y == null) continue
          ctx.strokeStyle = s.color
          ctx.lineWidth = s.width * hr
          ctx.setLineDash(s.dash.map(d => d * hr))
          ctx.beginPath()
          ctx.moveTo(Math.min(x1, x2) * hr, y * vr)
          ctx.lineTo(Math.max(x1, x2) * hr, y * vr)
          ctx.stroke()
          ctx.setLineDash([])
          if (s.label) {
            ctx.fillStyle = s.color
            ctx.fillText(s.label, Math.min(x1, x2) * hr + 4 * hr, y * vr - 2 * vr)
          }
        }
      }
    })
  }
}

let chart: IChartApi | null = null
let candle: ISeriesApi<'Candlestick'> | null = null
const overlay = new Overlay()

function build() {
  if (!el.value) return
  chart?.remove()
  chart = createChart(el.value, {
    autoSize: true,
    layout: { background: { type: ColorType.Solid, color: '#1d1e1f' }, textColor: '#b0b0b0' },
    grid: { vertLines: { color: '#2a2a2a' }, horzLines: { color: '#2a2a2a' } },
    timeScale: { timeVisible: true, secondsVisible: false },
    crosshair: { mode: 0 },
  })
  candle = chart.addCandlestickSeries({
    upColor: GREEN, downColor: RED, borderVisible: false, wickUpColor: GREEN, wickDownColor: RED,
  })
  candle.attachPrimitive(overlay)
}

function render(refit: boolean) {
  if (!chart || !candle) return
  const cs = props.candles
  candle.setData(cs.map(c => ({ time: (c.t + tz) as Time, open: c.o, high: c.h, low: c.l, close: c.c })))
  const firstT = cs.length ? cs[0].t + tz : 0
  const lastT = cs.length ? cs[cs.length - 1].t + tz : 0

  const shapes: Shape[] = []
  const markers: SeriesMarker<Time>[] = []
  for (const s of props.setups) {
    const long = s.dir === 'long'
    const col = long ? GREEN : RED
    const focus = s.id === props.focusId
    const w = focus ? 2 : 1
    const arm = barTime(unix(s.arm_time))
    const end = barTime(unix(s.end_time))
    const dim = s.status === 'expired' || s.status === 'missed' || s.status === 'invalid'

    shapes.push({
      kind: 'rect', t1: barTime(unix(s.zone_time)), t2: end, top: s.zone_top, bottom: s.zone_bottom,
      fill: (long ? 'rgba(38,166,154,' : 'rgba(239,83,80,') + (focus ? '0.28)' : dim ? '0.08)' : '0.18)'),
      stroke: (long ? 'rgba(38,166,154,' : 'rgba(239,83,80,') + (dim ? '0.35)' : '0.8)'), width: w,
    })
    shapes.push({ kind: 'line', t1: arm, t2: end, price: s.entry, color: '#cfcfcf', dash: [4, 3], width: w, label: `${s.zone_kind} 入场` })
    shapes.push({ kind: 'line', t1: arm, t2: end, price: s.stop, color: RED, dash: [], width: w, label: '止损' })
    shapes.push({ kind: 'line', t1: arm, t2: end, price: s.tp, color: GREEN, dash: [], width: w, label: `止盈 ${s.planned_rr.toFixed(1)}R` })

    const below = long ? 'belowBar' as const : 'aboveBar' as const
    const above = long ? 'aboveBar' as const : 'belowBar' as const
    const breakout = s.model === 'breakout'
    if (!breakout) {
      markers.push({ time: barTime(unix(s.sweep_time)) as Time, position: below, color: '#f0b90b', shape: 'circle', text: s.sweep_kind })
    }
    markers.push({ time: barTime(unix(s.mss_time)) as Time, position: below, color: '#2196f3', shape: long ? 'arrowUp' : 'arrowDown', text: breakout ? 'BOS' : 'MSS' })
    if (s.fill_time) {
      markers.push({ time: barTime(unix(s.fill_time)) as Time, position: below, color: '#ffffff', shape: 'square', text: '入场' })
    }
    if (s.exit_time && (s.status === 'tp' || s.status === 'sl' || s.status === 'timeout')) {
      const win = s.status === 'tp'
      markers.push({
        time: barTime(unix(s.exit_time)) as Time,
        position: win ? above : below,
        color: win ? GREEN : s.status === 'sl' ? RED : '#9e9e9e',
        shape: 'circle',
        text: `${s.status === 'tp' ? '止盈' : s.status === 'sl' ? '止损' : '超时'} ${s.r >= 0 ? '+' : ''}${s.r.toFixed(2)}R`,
      })
    }
  }
  markers.sort((a, b) => (a.time as number) - (b.time as number))
  candle.setMarkers(markers)
  overlay.set(shapes, firstT, lastT)

  if (refit && cs.length) {
    if (cs.length > 500) {
      chart.timeScale().setVisibleLogicalRange({ from: cs.length - 400, to: cs.length + 6 })
    } else {
      chart.timeScale().fitContent()
    }
  }
}

function focus() {
  if (!chart || !props.focusId) return
  const s = props.setups.find(x => x.id === props.focusId)
  if (!s) return
  chart.timeScale().setVisibleRange({
    from: (barTime(unix(s.sweep_time)) - 36 * 3600) as Time,
    to: (barTime(unix(s.end_time)) + 24 * 3600) as Time,
  })
}

onMounted(() => {
  build()
  render(true)
})
watch(() => props.candles, async () => { await nextTick(); render(true) })
watch(() => props.setups, async () => { await nextTick(); render(false) })
watch(() => props.focusId, async () => { await nextTick(); render(false); focus() })
onBeforeUnmount(() => { chart?.remove(); chart = null })
</script>
