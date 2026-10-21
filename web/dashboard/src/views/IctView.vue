<template>
  <div>
    <el-card shadow="never" class="card">
      <template #header>
        <div class="row-between">
          <span class="title">
            ICT 信号（规则版）
            <el-tag type="warning" size="small" style="margin-left: 8px">仅提示</el-tag>
            <el-tag size="small" style="margin-left: 8px">BTCUSDT</el-tag>
            <el-tag v-if="scan" :type="biasType(scan.bias_now)" size="small" style="margin-left: 8px">
              4h 结构：{{ biasName[scan.bias_now] }}
            </el-tag>
          </span>
          <span class="muted">
            更新于 {{ scan ? fmtTime(scan.generated_at) : '-' }}
            <el-button size="small" link :loading="scanLoading" @click="loadScan" style="margin-left: 8px">刷新</el-button>
          </span>
        </div>
      </template>

      <el-collapse class="rules">
        <el-collapse-item title="规则说明（点击展开）" name="rules">
          <div class="rule-text">
            <p><b>模型</b>：扫荡流动性 → 位移 + 结构转变（MSS）→ 回踩 FVG / 订单块入场。全部为确定性规则，不含 AI，不考虑 Kill Zone。</p>
            <p><b>流动性</b>：已确认的摆动高低点、等高/等低点（EQH/EQL）、前一日高低点（PDH/PDL）。影线刺破、收盘收回才算扫荡。</p>
            <p><b>MSS</b>：扫荡后 N 根内，一根实体 ≥ k×ATR 的强势K线，收盘突破最近的反向摆动点。多单在下一根K线收盘后生效，此时位移产生的 FVG 才完整。</p>
            <p><b>顺势突破模型（可选，默认关）</b>：不要求扫荡。一根位移K线首次收盘突破最近的摆动高点、且顺 4h 偏向时，用它留下的 FVG 回踩入场，止损放在这根K线及前一根的低点之下。图上标记为 BOS。价格不回踩就是“错过”。</p>
            <p><b>入场</b>：优先未被完全回补的最大 FVG，没有则取位移前最后一根反向K线（OB）。OB 一旦被价格回补过（形成后又有K线回到它的上沿/下沿）就直接弃掉；挂单价 = 区域内按“入场深度”取点，成交按 15m K线。</p>
            <p><b>止损</b>：扫荡极值外加 ATR 缓冲。<b>止盈</b>：最近且盈亏比 ≥ 最小值的流动性（BSL/SSL/PDH/PDL），没有则取固定盈亏比（rr）。</p>
            <p><b>回测口径</b>：同一根15m同时触及止损和止盈按止损算；成交那根K线只判止损；含手续费；已完成的信号才计入胜率。</p>
          </div>
        </el-collapse-item>
      </el-collapse>

      <el-alert v-if="scanErr" :title="scanErr" type="error" show-icon :closable="false" style="margin-bottom: 12px" />

      <div v-if="scan" class="kv-row">
        <span>最新价 <b>{{ scan.last_price.toFixed(2) }}</b></span>
        <span>近 {{ scan.days }} 天信号 <b>{{ scan.setups.length }}</b></span>
        <span>待触发/持仓 <b>{{ active.length }}</b></span>
        <span class="muted">图中：黄圈=扫荡，蓝箭头=MSS，色块=FVG/OB，虚线=入场，红线=止损，绿线=止盈</span>
      </div>

      <div class="cap">当前待触发 / 持仓中的信号</div>
      <IctSetupTable :rows="active" empty="当前没有待触发或持仓中的信号" @select="r => (scanFocus = r.id)" />

      <div v-if="scan" style="margin-top: 14px">
        <IctChart :candles="scan.candles" :setups="scan.setups" :focus-id="scanFocus" :height="460" />
      </div>
    </el-card>

    <el-card shadow="never" class="card">
      <template #header><span class="title">历史回测（Binance 历史K线，1h 结构 + 15m 成交）</span></template>

      <el-form :inline="true" size="small" label-position="top">
        <el-form-item label="回测天数">
          <el-select v-model="days" style="width: 100px">
            <el-option v-for="d in [30, 90, 180, 365]" :key="d" :label="d + ' 天'" :value="d" />
          </el-select>
        </el-form-item>
        <el-form-item label="方向">
          <el-select v-model="p.direction" style="width: 90px">
            <el-option label="多空" value="both" /><el-option label="仅多" value="long" /><el-option label="仅空" value="short" />
          </el-select>
        </el-form-item>
        <el-form-item label="摆动点宽度 N"><el-input-number v-model="p.swing_n" :min="1" :max="10" style="width: 110px" /></el-form-item>
        <el-form-item label="位移 ≥ ATR×"><el-input-number v-model="p.disp_atr" :min="0.3" :max="5" :step="0.1" :precision="1" style="width: 120px" /></el-form-item>
        <el-form-item label="扫荡→MSS 最多(根)"><el-input-number v-model="p.max_sweep_to_mss" :min="1" :max="48" style="width: 120px" /></el-form-item>
        <el-form-item label="最小盈亏比"><el-input-number v-model="p.min_rr" :min="0.5" :max="10" :step="0.5" :precision="1" style="width: 110px" /></el-form-item>
        <el-form-item label="入场深度 0~1"><el-input-number v-model="p.entry_depth" :min="0" :max="1" :step="0.1" :precision="1" style="width: 110px" /></el-form-item>
        <el-form-item label="止损缓冲 ATR×"><el-input-number v-model="p.stop_buf_atr" :min="0" :max="2" :step="0.05" :precision="2" style="width: 120px" /></el-form-item>
        <el-form-item label="最大止损 %"><el-input-number v-model="p.max_risk_pct" :min="0.2" :max="20" :step="0.5" :precision="1" style="width: 110px" /></el-form-item>
        <el-form-item label="顺 4h 偏向"><el-switch v-model="p.require_bias" /></el-form-item>
        <el-form-item label="折价/溢价过滤"><el-switch v-model="p.require_discount" /></el-form-item>
        <el-form-item label="顺势突破模型"><el-switch v-model="p.breakout" /></el-form-item>
        <el-form-item label=" ">
          <el-button type="warning" :loading="btLoading" @click="runBt">运行回测</el-button>
          <el-button @click="resetParams">恢复默认</el-button>
        </el-form-item>
      </el-form>

      <div v-if="!bt" class="muted">选择参数后点击“运行回测”。首次拉取一年的 15m 数据约需十几秒，之后调参会走缓存。</div>

      <div v-if="bt">
        <div class="tiles">
          <div class="tile"><span>信号</span><b>{{ bt.stats.armed }}</b></div>
          <div class="tile"><span>成交 / 成交率</span><b>{{ bt.stats.filled }} / {{ (bt.stats.fill_rate * 100).toFixed(0) }}%</b></div>
          <div class="tile"><span>已结束</span><b>{{ bt.stats.resolved }}</b></div>
          <div class="tile"><span>胜率</span><b>{{ (bt.stats.win_rate * 100).toFixed(1) }}%</b></div>
          <div class="tile"><span>平均 R</span><b :class="bt.stats.avg_r >= 0 ? 'win' : 'loss'">{{ bt.stats.avg_r.toFixed(2) }}</b></div>
          <div class="tile"><span>总 R</span><b :class="bt.stats.total_r >= 0 ? 'win' : 'loss'">{{ bt.stats.total_r.toFixed(1) }}</b></div>
          <div class="tile"><span>盈亏比(PF)</span><b>{{ bt.stats.profit_factor ? bt.stats.profit_factor.toFixed(2) : '-' }}</b></div>
          <div class="tile"><span>最大回撤</span><b class="loss">{{ bt.stats.max_drawdown_r.toFixed(1) }}R</b></div>
        </div>
        <div class="muted" style="margin: 8px 0">
          止盈 {{ bt.stats.tp }} · 止损 {{ bt.stats.sl }} · 超时 {{ bt.stats.timeout }} · 未成交 {{ bt.stats.expired }} · 错过 {{ bt.stats.missed }} · 持仓中 {{ bt.stats.open }} · 待触发 {{ bt.stats.pending }}
          ｜ 漏斗：扫荡 {{ bt.funnel.sweeps }} → 结构转变 {{ bt.funnel.shifts }}<template v-if="bt.params.breakout">（另有顺势突破 {{ bt.funnel.breakouts }}）</template> → 入场信号 {{ bt.funnel.armed }}
          <template v-if="rejectedText">｜ 被过滤：{{ rejectedText }}</template>
        </div>
        <el-alert
          v-if="bt.stats.resolved < 30"
          type="warning" show-icon :closable="false" style="margin-bottom: 10px"
          :title="`已结束的交易只有 ${bt.stats.resolved} 笔，样本太少，胜率和平均 R 没有统计意义，不要据此调参。`"
        />

        <div class="cap">累计 R 曲线（按平仓时间）</div>
        <div ref="eqEl" style="height: 180px"></div>

        <div class="cap">价格与信号（点击下表某行可定位）</div>
        <IctChart :candles="bt.candles" :setups="bt.setups" :focus-id="btFocus" :height="460" />

        <div class="cap">分组统计（仅已结束交易）</div>
        <el-table :data="bt.buckets" size="small" :header-cell-style="hdr" max-height="260">
          <el-table-column label="维度" width="100"><template #default="{ row }">{{ groupName[row.group] ?? row.group }}</template></el-table-column>
          <el-table-column label="分组" width="140"><template #default="{ row }">{{ nameText[row.name] ?? row.name }}</template></el-table-column>
          <el-table-column prop="count" label="笔数" width="80" />
          <el-table-column label="胜率" width="90"><template #default="{ row }">{{ (row.win_rate * 100).toFixed(0) }}%</template></el-table-column>
          <el-table-column label="平均R" width="90"><template #default="{ row }">{{ row.avg_r.toFixed(2) }}</template></el-table-column>
          <el-table-column label="总R"><template #default="{ row }">{{ row.total_r.toFixed(1) }}</template></el-table-column>
        </el-table>

        <div class="cap">信号明细（最新在前）</div>
        <IctSetupTable :rows="btRows" empty="没有信号，尝试放宽参数" @select="r => (btFocus = r.id)" />
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { createChart, ColorType, type IChartApi, type Time } from 'lightweight-charts'
import IctChart from '@/components/charts/IctChart.vue'
import IctSetupTable from '@/components/charts/IctSetupTable.vue'
import { defaultIctParams, fetchIctScan, runIctBacktest, type IctResult } from '@/api/ict'

const biasName = { up: '偏多', down: '偏空', none: '未定' }
const groupName: Record<string, string> = { model: '模型', direction: '方向', zone: '入场区域', sweep: '触发类型', target: '止盈类型' }
const nameText: Record<string, string> = { reversal: '反转(扫荡+MSS)', breakout: '顺势突破', long: '做多', short: '做空', rr: '固定盈亏比' }
const rejectName: Record<string, string> = {
  counter_bias: '逆4h偏向', not_discount: '不在折价/溢价区', busy: '已有信号占用', no_zone: '无FVG/OB',
  retraced_early: '确认前已回踩', ob_mitigated: 'OB已被回补', risk_too_wide: '止损过宽', risk_too_small: '止损过窄',
}
const hdr = { background: '#232425', color: '#b0b0b0' }
const tz = -new Date().getTimezoneOffset() * 60

function biasType(b: string) { return b === 'up' ? 'success' : b === 'down' ? 'danger' : 'info' }
function fmtTime(iso: string) {
  const d = new Date(iso)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

// ---------- live scan ----------
const scan = ref<IctResult | null>(null)
const scanLoading = ref(false)
const scanErr = ref('')
const scanFocus = ref('')
const active = computed(() => (scan.value?.setups ?? []).filter(s => s.status === 'pending' || s.status === 'open').reverse())
let timer = 0

async function loadScan() {
  scanLoading.value = true
  try {
    scan.value = await fetchIctScan(14)
    scanErr.value = ''
  } catch {
    scanErr.value = '加载失败：后端未启动或 Binance 行情接口不可达'
  } finally {
    scanLoading.value = false
  }
}

// ---------- backtest ----------
const days = ref(90)
const p = reactive(defaultIctParams())
const bt = ref<IctResult | null>(null)
const btLoading = ref(false)
const btFocus = ref('')
const btRows = computed(() => [...(bt.value?.setups ?? [])].reverse())
const rejectedText = computed(() =>
  Object.entries(bt.value?.rejected ?? {}).map(([k, v]) => `${rejectName[k] ?? k} ${v}`).join('、'),
)

function resetParams() { Object.assign(p, defaultIctParams()) }

async function runBt() {
  btLoading.value = true
  btFocus.value = ''
  try {
    bt.value = await runIctBacktest(days.value, { ...p })
  } catch {
    ElMessage.error('回测失败')
  } finally {
    btLoading.value = false
  }
}

const eqEl = ref<HTMLElement | null>(null)
let eqChart: IChartApi | null = null
watch(bt, async () => {
  await nextTick()
  eqChart?.remove()
  eqChart = null
  if (!bt.value || !eqEl.value) return
  eqChart = createChart(eqEl.value, {
    autoSize: true,
    layout: { background: { type: ColorType.Solid, color: '#1d1e1f' }, textColor: '#b0b0b0' },
    grid: { vertLines: { color: '#2a2a2a' }, horzLines: { color: '#2a2a2a' } },
    timeScale: { timeVisible: true, secondsVisible: false },
  })
  const line = eqChart.addLineSeries({ color: '#f0b90b', lineWidth: 2 })
  line.setData(bt.value.equity.map(e => ({ time: (e.t + tz) as Time, value: e.r })))
  line.createPriceLine({ price: 0, color: '#666', lineWidth: 1, lineStyle: 2, axisLabelVisible: false, title: '' })
  eqChart.timeScale().fitContent()
})

onMounted(() => {
  loadScan()
  timer = window.setInterval(loadScan, 120000)
})
onBeforeUnmount(() => {
  clearInterval(timer)
  eqChart?.remove()
})
</script>

<style scoped>
.card { background: #1d1e1f; border-color: #333; margin-bottom: 16px }
.title { color: #f0b90b; font-weight: 600 }
.row-between { display: flex; justify-content: space-between; align-items: center }
.muted { color: #888; font-size: 12px }
.cap { color: #b0b0b0; font-size: 12px; margin: 16px 0 6px }
.rules { margin-bottom: 12px; border-color: #333 }
.rule-text { color: #b0b0b0; font-size: 13px; line-height: 1.7 }
.rule-text p { margin: 4px 0 }
.kv-row { display: flex; flex-wrap: wrap; gap: 20px; color: #b0b0b0; font-size: 13px; margin-bottom: 4px }
.kv-row b { color: #eee }
.tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(120px, 1fr)); gap: 10px; margin-top: 8px }
.tile { border: 1px solid #333; border-radius: 6px; padding: 8px 12px; background: #232425; display: flex; flex-direction: column; gap: 4px }
.tile span { color: #888; font-size: 12px }
.tile b { color: #eee; font-size: 16px }
.win { color: #26a69a !important }
.loss { color: #ef5350 !important }
</style>
