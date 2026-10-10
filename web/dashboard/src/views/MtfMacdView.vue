<template>
  <div>
    <el-card shadow="never" class="card">
      <template #header>
        <div class="row-between">
          <span class="title">
            三周期共振（MACD {{ status?.macd.fast }}/{{ status?.macd.slow }}/{{ status?.macd.signal }}）
            <el-tag type="warning" size="small" style="margin-left: 8px">仅提示</el-tag>
            <el-tag size="small" style="margin-left: 8px">{{ status?.symbol }}</el-tag>
          </span>
          <span class="muted">
            更新于 {{ status?.updated_at ? fmtTime(status.updated_at) : '-' }}
            <el-button size="small" link @click="loadAll" style="margin-left: 8px">刷新</el-button>
          </span>
        </div>
      </template>

      <el-alert v-if="status?.error" :title="status.error" type="error" show-icon :closable="false" style="margin-bottom: 12px" />

      <div class="opt">
        <el-switch v-model="beyondZero" @change="onToggle" />
        <span style="margin-left: 8px">
          15m 0轴过滤：做多要求 15m 金叉时 DIF 在 0 轴<b>下方</b>，做空要求死叉时 DIF 在 0 轴<b>上方</b>
          （回调/反弹结束才入场，不追涨杀跌）
        </span>
      </div>

      <el-row :gutter="12" v-if="ev">
        <el-col :span="8" v-for="tf in tfs" :key="tf.interval">
          <div class="tf" :class="tf.trend">
            <div class="tf-head">
              <span class="tf-name">{{ tf.interval }}</span>
              <el-tag size="small" :type="trendTag(tf.trend)">{{ trendText(tf.trend) }}</el-tag>
            </div>
            <div class="kv"><span>DIF</span><b :class="cls(tf.macd)">{{ tf.macd.toFixed(2) }}</b></div>
            <div class="kv"><span>DEA</span><b>{{ tf.signal.toFixed(2) }}</b></div>
            <div class="kv"><span>柱</span><b :class="cls(tf.histogram)">{{ tf.histogram.toFixed(2) }}</b></div>
            <div class="kv"><span>0轴</span><b>{{ tf.macd >= 0 ? '上方' : '下方' }}</b></div>
          </div>
        </el-col>
      </el-row>

      <div v-if="ev" class="verdict" :class="verdictClass">
        <div class="v-main">{{ verdict }}</div>
        <div class="muted">{{ ev.note }}　最新15m收盘 {{ fmtTime(ev.time) }}　价格 {{ ev.price.toFixed(2) }}</div>
      </div>
    </el-card>

    <el-card shadow="never" class="card">
      <template #header><span class="title">实时信号记录（本次启动后）</span></template>
      <el-table :data="signals" size="small" empty-text="暂无信号" :header-cell-style="hdr">
        <el-table-column label="15m收盘时间" width="150">
          <template #default="{ row }">{{ fmtTime(row.time) }}</template>
        </el-table-column>
        <el-table-column label="方向" width="90">
          <template #default="{ row }">
            <el-tag :type="row.direction === 'long' ? 'success' : 'danger'" size="small">
              {{ row.direction === 'long' ? '做多' : '做空' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="价格" width="110"><template #default="{ row }">{{ row.price.toFixed(2) }}</template></el-table-column>
        <el-table-column label="4h DIF" width="90"><template #default="{ row }">{{ row.h4.macd.toFixed(1) }}</template></el-table-column>
        <el-table-column label="1h DIF" width="90"><template #default="{ row }">{{ row.h1.macd.toFixed(1) }}</template></el-table-column>
        <el-table-column label="15m DIF" width="90"><template #default="{ row }">{{ row.m15.macd.toFixed(1) }}</template></el-table-column>
        <el-table-column prop="note" label="说明" />
      </el-table>
    </el-card>

    <el-card shadow="never" class="card">
      <template #header><span class="title">历史回测（Binance 历史K线，信号后 N 根的方向收益，未计手续费）</span></template>

      <el-form :inline="true" size="small" label-position="top">
        <el-form-item label="回测天数">
          <el-select v-model="opts.days" style="width: 100px">
            <el-option v-for="d in [30, 90, 180, 365]" :key="d" :label="d + ' 天'" :value="d" />
          </el-select>
        </el-form-item>
        <el-form-item label="15m 反转定义">
          <el-select v-model="opts.reversal" style="width: 210px">
            <el-option label="金叉/死叉（柱子翻转）" value="cross" />
            <el-option label="DIF 拐头" value="turn" />
            <el-option label="DIF 拐头（金叉前）" value="turn_pre" />
          </el-select>
        </el-form-item>
        <el-form-item label="15m 背离过滤">
          <el-select v-model="opts.divergence" style="width: 230px">
            <el-option label="不过滤" value="none" />
            <el-option label="隐性背离（价格低点抬高、DIF低点更低）" value="hidden" />
            <el-option label="DIF 低点抬高" value="dif_up" />
            <el-option label="常规背离（价格新低、DIF不新低，信号极少）" value="regular" />
          </el-select>
        </el-form-item>
        <el-form-item label="仅做多">
          <el-switch v-model="opts.long_only" />
        </el-form-item>
        <el-form-item label="4h 柱子放大">
          <el-switch v-model="opts.expanding_4h" />
        </el-form-item>
        <el-form-item label="1h |DIF|/价格 ≥ %">
          <el-input-number v-model="opts.min_1h_dif_pct" :min="0" :max="2" :step="0.1" :precision="1" style="width: 120px" />
        </el-form-item>
        <el-form-item label=" ">
          <el-button type="warning" :loading="btLoading" @click="runBt">运行回测</el-button>
        </el-form-item>
      </el-form>

      <div v-if="!bt" class="muted">选择参数后点击"运行回测"。图表和明细都基于同一批信号，可点击行或图上的箭头查看该信号的 4h/1h/15m 走势。</div>

      <div v-if="bt">
        <div class="muted" style="margin-bottom: 8px">
          {{ bt.symbol }} · {{ bt.days }} 天 · 15m K线 {{ bt.bars_15m }} 根 · 共 {{ bt.signals.length }} 个信号。
          "0轴过滤"为同一批信号的子集，用来对比加不加该选项的差别。
        </div>
        <el-table :data="bt.buckets" size="small" :header-cell-style="hdr">
          <el-table-column prop="name" label="分组" width="190" />
          <el-table-column prop="count" label="信号数" width="80" />
          <el-table-column v-for="h in bt.horizons" :key="h" :label="horizonLabel(h)" min-width="130">
            <template #default="{ row }">
              <template v-if="row.by_horizon[h]?.samples">
                <div>胜率 <b :class="row.by_horizon[h].win_rate >= 50 ? 'up' : 'down'">{{ row.by_horizon[h].win_rate.toFixed(1) }}%</b></div>
                <div class="muted">均 {{ row.by_horizon[h].avg_return_pct.toFixed(2) }}% · 盈 {{ row.by_horizon[h].avg_win_pct.toFixed(2) }} / 亏 {{ row.by_horizon[h].avg_loss_pct.toFixed(2) }}</div>
              </template>
              <span v-else class="muted">-</span>
            </template>
          </el-table-column>
          <el-table-column label="平均最大浮盈/浮亏" width="150">
            <template #default="{ row }">
              <span class="up">+{{ row.avg_mfe_pct.toFixed(2) }}%</span> / <span class="down">{{ row.avg_mae_pct.toFixed(2) }}%</span>
            </template>
          </el-table-column>
        </el-table>

        <div class="chart-bar">
          <span>图表持有时长：</span>
          <el-radio-group v-model="hz" size="small">
            <el-radio-button v-for="h in bt.horizons" :key="h" :value="h">{{ horizonLabel(h).replace(' 后', '') }}</el-radio-button>
          </el-radio-group>
          <span v-if="!bt.params.LongOnly" style="margin-left: 16px">方向：</span>
          <el-radio-group v-if="!bt.params.LongOnly" v-model="dirFilter" size="small">
            <el-radio-button value="all">全部</el-radio-button>
            <el-radio-button value="long">做多</el-radio-button>
            <el-radio-button value="short">做空</el-radio-button>
          </el-radio-group>
        </div>

        <div class="cap">价格（1h K线）与信号位置：绿色↑做多，红色↓做空（点击箭头附近查看明细）</div>
        <div ref="priceEl" style="height: 380px"></div>

        <div class="cap">
          累计收益曲线（按所选持有时长，各信号收益直接累加，未复利、未扣费；信号之间有重叠，仅用于看趋势是否稳定）
          　合计 <b :class="eqTotal >= 0 ? 'up' : 'down'">{{ eqTotal.toFixed(1) }}%</b>
        </div>
        <div ref="eqEl" style="height: 220px"></div>

        <div class="cap">收益分布（所选持有时长，单位 %）</div>
        <div class="dist">
          <div v-for="b in distBins" :key="b.label" class="dist-col">
            <div class="dist-bar" :style="{ height: b.pct + '%', background: b.neg ? '#ef5350' : '#26a69a' }" :title="b.count + ' 笔'"></div>
            <div class="dist-n">{{ b.count }}</div>
            <div class="dist-l">{{ b.label }}</div>
          </div>
        </div>

        <div class="cap" style="margin-top: 14px">信号明细（点击行查看三周期走势）</div>
        <el-table :data="shownSignals" size="small" max-height="420" :header-cell-style="hdr" highlight-current-row @row-click="openWindow">
          <el-table-column label="时间" width="140"><template #default="{ row }">{{ fmtTime(row.time) }}</template></el-table-column>
          <el-table-column label="方向" width="70">
            <template #default="{ row }"><span :class="row.direction === 'long' ? 'up' : 'down'">{{ row.direction === 'long' ? '做多' : '做空' }}</span></template>
          </el-table-column>
          <el-table-column label="价格" width="90"><template #default="{ row }">{{ row.price.toFixed(2) }}</template></el-table-column>
          <el-table-column label="4h DIF" width="80"><template #default="{ row }">{{ row.h4_dif.toFixed(0) }}</template></el-table-column>
          <el-table-column label="1h DIF" width="80"><template #default="{ row }">{{ row.h1_dif.toFixed(0) }}</template></el-table-column>
          <el-table-column label="15m DIF" width="80"><template #default="{ row }">{{ row.m15_dif.toFixed(0) }}</template></el-table-column>
          <el-table-column label="0轴条件" width="80"><template #default="{ row }">{{ row.beyond_zero ? '满足' : '-' }}</template></el-table-column>
          <el-table-column v-for="h in bt.horizons" :key="h" :label="horizonLabel(h)" width="85">
            <template #default="{ row }">
              <span v-if="row.returns[h] != null" :class="row.returns[h]! > 0 ? 'up' : 'down'">{{ row.returns[h]!.toFixed(2) }}%</span>
              <span v-else class="muted">-</span>
            </template>
          </el-table-column>
          <el-table-column label="最大浮盈" width="85"><template #default="{ row }"><span class="up">+{{ row.mfe_pct.toFixed(2) }}%</span></template></el-table-column>
          <el-table-column label="最大浮亏" width="85"><template #default="{ row }"><span class="down">{{ row.mae_pct.toFixed(2) }}%</span></template></el-table-column>
        </el-table>
      </div>
    </el-card>

    <el-dialog v-model="dlg" width="1100px" top="4vh" :title="dlgTitle" destroy-on-close>
      <div v-if="dlgLoading" class="muted">加载中...</div>
      <div v-else-if="win && curSig">
        <MacdBarChart title="15m（黄=DIF，蓝=DEA，柱=Histogram）" :bars="win.m15" :mark-time="mark15" :mark-dir="curSig.direction" :height="360" />
        <MacdBarChart title="1h" :bars="win.h1" :mark-time="mark1h" :mark-dir="curSig.direction" :height="300" />
        <MacdBarChart title="4h" :bars="win.h4" :mark-time="mark4h" :mark-dir="curSig.direction" :height="300" />
      </div>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { createChart, ColorType, type IChartApi } from 'lightweight-charts'
import MacdBarChart from '@/components/charts/MacdBarChart.vue'
import {
  fetchMtfSignals, fetchMtfStatus, fetchMtfWindow, runMtfBacktest, setMtfConfig,
  type BtOptions, type BtSignal, type MtfBacktest, type MtfSignal, type MtfStatus, type MtfWindow,
} from '@/api/mtfMacd'

const hdr = { background: '#262727', color: '#b0b0b0' }
const status = ref<MtfStatus | null>(null)
const signals = ref<MtfSignal[]>([])
const beyondZero = ref(false)
const opts = reactive<BtOptions>({ days: 365, reversal: 'turn_pre', expanding_4h: false, min_1h_dif_pct: 0, long_only: true, divergence: 'none' })
const bt = ref<MtfBacktest | null>(null)
const btLoading = ref(false)
const hz = ref(96)
const dirFilter = ref<'all' | 'long' | 'short'>('all')
let timer: number | undefined

const ev = computed(() => status.value?.eval ?? null)
const tfs = computed(() => (ev.value ? [ev.value.h4, ev.value.h1, ev.value.m15] : []))

const verdict = computed(() => {
  const e = ev.value
  if (!e) return ''
  if (e.triggered) return e.direction === 'long' ? '🟢 三周期共振：做多信号' : '🔴 三周期共振：做空信号'
  if (e.htf_trend === 'up') return '二周期共振：多头（等待 15m 金叉）'
  if (e.htf_trend === 'down') return '二周期共振：空头（等待 15m 死叉）'
  return '无共振'
})
const verdictClass = computed(() => {
  const e = ev.value
  if (!e) return ''
  if (e.triggered) return e.direction === 'long' ? 'hit-long' : 'hit-short'
  return e.htf_trend === 'none' ? '' : 'watch'
})

const trendText = (t: string) => (t === 'up' ? '多头' : t === 'down' ? '空头' : '无方向')
const trendTag = (t: string) => (t === 'up' ? 'success' : t === 'down' ? 'danger' : 'info')
const cls = (v: number) => (v >= 0 ? 'up' : 'down')
const horizonLabel = (h: number) => (h < 4 ? `${h * 15}m` : `${h / 4}h`) + ' 后'
const fmtTime = (s: string) => new Date(s).toLocaleString('zh-CN', { hour12: false, month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })

async function loadAll() {
  try {
    status.value = await fetchMtfStatus()
    beyondZero.value = status.value.require_15m_beyond_zero
    signals.value = await fetchMtfSignals()
  } catch { /* shown by http interceptor log */ }
}

async function onToggle(v: boolean | string | number) {
  try {
    await setMtfConfig(!!v)
    ElMessage.success('已更新（重启后恢复为配置文件默认值）')
    await loadAll()
  } catch {
    beyondZero.value = !v
    ElMessage.error('更新失败')
  }
}

// ---------- backtest ----------
const tz = -new Date().getTimezoneOffset() * 60
const unix = (s: string) => Math.floor(new Date(s).getTime() / 1000)

const shownSignals = computed<BtSignal[]>(() =>
  (bt.value?.signals ?? []).filter(s => dirFilter.value === 'all' || s.direction === dirFilter.value))
const retOf = (s: BtSignal) => s.returns[hz.value] ?? null

const eqSeries = computed(() => {
  const asc = [...shownSignals.value].reverse()
  let acc = 0
  const out: { time: number; value: number }[] = []
  for (const s of asc) {
    const r = retOf(s)
    if (r == null) continue
    acc += r
    out.push({ time: unix(s.time) + tz, value: +acc.toFixed(3) })
  }
  return out
})
const eqTotal = computed(() => eqSeries.value[eqSeries.value.length - 1]?.value ?? 0)

const distBins = computed(() => {
  const rs = shownSignals.value.map(retOf).filter((v): v is number => v != null)
  if (!rs.length) return []
  const lim = 6, step = 0.5
  const n = (lim * 2) / step
  const counts = new Array(n).fill(0)
  for (const r of rs) {
    const i = Math.min(n - 1, Math.max(0, Math.floor((Math.max(-lim, Math.min(lim - 1e-9, r)) + lim) / step)))
    counts[i]++
  }
  const mx = Math.max(...counts, 1)
  return counts.map((c, i) => {
    const lo = -lim + i * step
    return { label: i % 2 === 0 ? String(lo) : '', count: c, pct: (c / mx) * 100, neg: lo < 0 }
  })
})

const priceEl = ref<HTMLElement | null>(null)
const eqEl = ref<HTMLElement | null>(null)
let priceChart: IChartApi | null = null
let eqChart: IChartApi | null = null

const baseOpts = () => ({
  layout: { background: { type: ColorType.Solid, color: '#1d1e1f' }, textColor: '#b0b0b0' },
  grid: { vertLines: { color: '#2a2a2a' }, horzLines: { color: '#2a2a2a' } },
  timeScale: { timeVisible: true, secondsVisible: false },
})

function drawCharts() {
  const b = bt.value
  if (!b || !priceEl.value || !eqEl.value) return
  priceChart?.remove(); eqChart?.remove()

  priceChart = createChart(priceEl.value, baseOpts())
  const candle = priceChart.addCandlestickSeries({
    upColor: '#26a69a', downColor: '#ef5350', borderVisible: false, wickUpColor: '#26a69a', wickDownColor: '#ef5350',
  })
  candle.setData(b.candles.map(c => ({ time: (c.t + tz) as any, open: c.o, high: c.h, low: c.l, close: c.c })))
  const mk = [...shownSignals.value].reverse().map(s => {
    const long = s.direction === 'long'
    return {
      time: (Math.floor(unix(s.time) / 3600) * 3600 + tz) as any,
      position: long ? 'belowBar' as const : 'aboveBar' as const,
      color: long ? '#26a69a' : '#ef5350',
      shape: long ? 'arrowUp' as const : 'arrowDown' as const,
    }
  })
  candle.setMarkers(mk)
  priceChart.timeScale().fitContent()
  priceChart.subscribeClick(param => {
    if (param.time == null) return
    const t = (param.time as number) - tz
    // nearest signal within 12h of the clicked 1h candle
    let best: BtSignal | null = null, bd = 12 * 3600
    for (const s of shownSignals.value) {
      const d = Math.abs(unix(s.time) - t)
      if (d < bd) { bd = d; best = s }
    }
    if (best) openWindow(best)
  })

  eqChart = createChart(eqEl.value, baseOpts())
  const line = eqChart.addLineSeries({ color: '#f0b90b', lineWidth: 2 })
  line.setData(eqSeries.value as any)
  line.createPriceLine({ price: 0, color: '#666', lineWidth: 1, lineStyle: 2, axisLabelVisible: false, title: '' })
  eqChart.timeScale().fitContent()
}

watch([bt, hz, dirFilter], async () => { await nextTick(); drawCharts() })

async function runBt() {
  btLoading.value = true
  try {
    bt.value = await runMtfBacktest({ ...opts })
  } catch {
    ElMessage.error('回测失败')
  } finally {
    btLoading.value = false
  }
}

// ---------- signal window dialog ----------
const dlg = ref(false)
const dlgLoading = ref(false)
const win = ref<MtfWindow | null>(null)
const curSig = ref<BtSignal | null>(null)
const dlgTitle = computed(() => curSig.value
  ? `${fmtTime(curSig.value.time)} ${curSig.value.direction === 'long' ? '做多' : '做空'} @ ${curSig.value.price.toFixed(2)}`
  : '')
const mark15 = computed(() => curSig.value ? Math.floor(unix(curSig.value.time) / 900) * 900 : undefined)
const mark1h = computed(() => curSig.value ? Math.floor(unix(curSig.value.time) / 3600) * 3600 : undefined)
const mark4h = computed(() => curSig.value ? Math.floor(unix(curSig.value.time) / 14400) * 14400 : undefined)

async function openWindow(row: BtSignal) {
  curSig.value = row
  win.value = null
  dlg.value = true
  dlgLoading.value = true
  try {
    win.value = await fetchMtfWindow(row.time)
  } catch {
    ElMessage.error('加载走势失败')
  } finally {
    dlgLoading.value = false
  }
}

onMounted(() => {
  loadAll()
  timer = window.setInterval(loadAll, 30000)
})
onBeforeUnmount(() => {
  clearInterval(timer)
  priceChart?.remove(); eqChart?.remove()
})
</script>

<style scoped>
.card { background: #1d1e1f; border-color: #333; margin-bottom: 16px }
.title { color: #f0b90b; font-weight: 600 }
.row-between { display: flex; justify-content: space-between; align-items: center }
.muted { color: #888; font-size: 12px }
.opt { margin-bottom: 14px; color: #b0b0b0; font-size: 13px }
.tf { border: 1px solid #333; border-radius: 6px; padding: 12px; background: #232425 }
.tf.up { border-color: #2e7d32 }
.tf.down { border-color: #c62828 }
.tf-head { display: flex; justify-content: space-between; margin-bottom: 8px }
.tf-name { font-weight: 600; color: #ddd }
.kv { display: flex; justify-content: space-between; font-size: 13px; color: #888; line-height: 1.8 }
.up { color: #26a69a }
.down { color: #ef5350 }
.verdict { margin-top: 14px; padding: 12px 14px; border-radius: 6px; background: #232425; border: 1px solid #333 }
.verdict.watch { border-color: #f0b90b }
.verdict.hit-long { border-color: #26a69a; background: rgba(38, 166, 154, .12) }
.verdict.hit-short { border-color: #ef5350; background: rgba(239, 83, 80, .12) }
.cap { color: #b0b0b0; font-size: 12px; margin: 16px 0 4px }
.chart-bar { margin-top: 16px; color: #b0b0b0; font-size: 13px }
.dist { display: flex; align-items: flex-end; gap: 2px; height: 130px; padding-top: 4px }
.dist-col { flex: 1; display: flex; flex-direction: column; justify-content: flex-end; align-items: center; height: 100% }
.dist-bar { width: 100%; min-height: 1px; border-radius: 2px 2px 0 0 }
.dist-n { font-size: 10px; color: #888; height: 12px }
.dist-l { font-size: 10px; color: #888; height: 12px }
.v-main { font-size: 16px; font-weight: 600; color: #eee; margin-bottom: 4px }
</style>
