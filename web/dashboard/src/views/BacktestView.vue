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
        <el-form-item label="Days">
          <el-input-number v-model="form.days" :min="1" :max="365" style="width: 110px" />
        </el-form-item>
        <el-form-item label="Initial Cash">
          <el-input-number v-model="form.cash" :min="100" :step="1000" style="width: 140px" />
        </el-form-item>
        <el-form-item label="Alloc %">
          <el-input-number v-model="allocPct" :min="1" :max="100" :step="5" style="width: 100px" />
        </el-form-item>
        <el-form-item label="Fee %">
          <el-input-number v-model="feePct" :min="0" :max="1" :step="0.01" :precision="3" style="width: 100px" />
        </el-form-item>
        <el-form-item label=" " style="margin-top: 4px">
          <el-button type="primary" :loading="loading" @click="runBacktest" style="background: #f0b90b; border-color: #f0b90b; color: #000">
            Run Backtest
          </el-button>
        </el-form-item>
      </el-form>
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
              <div class="metric-sub">{{ result.metrics.total_return_pct >= 0 ? '+' : '' }}{{ result.metrics.total_return_pct.toFixed(2) }}%</div>
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

      <!-- Equity Curve -->
      <el-card shadow="never" style="background: #1d1e1f; border-color: #333; margin-bottom: 16px">
        <template #header>
          <span style="color: #e0e0e0">Equity Curve</span>
        </template>
        <div ref="chartContainer" style="width: 100%; height: 350px"></div>
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
              :cell-style="{ background: '#1d1e1f', color: '#e0e0e0' }"
            >
              <el-table-column label="Time" width="160">
                <template #default="{ row }">{{ formatTime(row.timestamp) }}</template>
              </el-table-column>
              <el-table-column prop="side" label="Side" width="70">
                <template #default="{ row }">
                  <el-tag :type="row.side === 'BUY' ? 'success' : 'danger'" size="small">{{ row.side }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column label="Price">
                <template #default="{ row }">{{ formatPrice(row.price) }}</template>
              </el-table-column>
              <el-table-column label="Qty">
                <template #default="{ row }">{{ row.quantity.toFixed(6) }}</template>
              </el-table-column>
              <el-table-column label="Fee">
                <template #default="{ row }">{{ row.fee.toFixed(4) }}</template>
              </el-table-column>
              <el-table-column label="PnL">
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
import { ref, computed, onMounted, nextTick, watch } from 'vue'
import { createChart, type IChartApi, type ISeriesApi, ColorType } from 'lightweight-charts'
import { runBacktest as apiRunBacktest, getStrategies, type BacktestResult, type StrategyInfo } from '@/api/backtest'
import { SYMBOLS, INTERVALS } from '@/utils/constants'
import { formatPrice, formatTime } from '@/utils/format'
import { ElMessage } from 'element-plus'

const form = ref({
  symbol: 'BTCUSDT',
  interval: '5m',
  strategy: 'ema_crossover',
  days: 30,
  cash: 10000,
})

const allocPct = ref(10)
const feePct = ref(0.1)
const loading = ref(false)
const result = ref<BacktestResult | null>(null)
const strategies = ref<StrategyInfo[]>([
  { name: 'ema_crossover', label: 'EMA Crossover' },
  { name: 'macd_rsi', label: 'MACD + RSI' },
  { name: 'bb_breakout', label: 'Bollinger Bands Breakout' },
])

const chartContainer = ref<HTMLElement | null>(null)
let chart: IChartApi | null = null
let lineSeries: ISeriesApi<'Area'> | null = null

function formatDate(s: string): string {
  const d = new Date(s)
  if (d.getFullYear() <= 1) return 'N/A'
  return d.toLocaleDateString()
}

async function runBacktest() {
  loading.value = true
  result.value = null
  try {
    const res = await apiRunBacktest({
      ...form.value,
      alloc: allocPct.value / 100,
      fee: feePct.value / 100,
    })
    result.value = res
    await nextTick()
    renderChart()
    ElMessage.success(`Backtest complete: ${res.metrics.total_trades} trades, ${res.metrics.total_return_pct.toFixed(2)}% return`)
  } catch (e: any) {
    ElMessage.error('Backtest failed: ' + (e.response?.data?.message || e.message))
  } finally {
    loading.value = false
  }
}

function renderChart() {
  if (!chartContainer.value || !result.value?.equity_curve?.length) return

  // Cleanup old chart
  if (chart) {
    chart.remove()
    chart = null
  }

  chart = createChart(chartContainer.value, {
    width: chartContainer.value.clientWidth,
    height: 350,
    layout: {
      background: { type: ColorType.Solid, color: '#1d1e1f' },
      textColor: '#b0b0b0',
    },
    grid: {
      vertLines: { color: '#2a2a2a' },
      horzLines: { color: '#2a2a2a' },
    },
    timeScale: {
      timeVisible: true,
      secondsVisible: false,
    },
    rightPriceScale: {
      borderColor: '#333',
    },
  })

  const tzOffsetSec = -new Date().getTimezoneOffset() * 60

  lineSeries = chart.addAreaSeries({
    lineColor: '#f0b90b',
    topColor: 'rgba(240, 185, 11, 0.3)',
    bottomColor: 'rgba(240, 185, 11, 0.02)',
    lineWidth: 2,
    priceFormat: { type: 'price', precision: 2, minMove: 0.01 },
  })

  const data = result.value.equity_curve.map(p => ({
    time: (Math.floor(new Date(p.time).getTime() / 1000) + tzOffsetSec) as any,
    value: p.equity,
  }))

  lineSeries.setData(data)
  chart.timeScale().fitContent()

  // Resize observer
  const ro = new ResizeObserver(() => {
    if (chart && chartContainer.value) {
      chart.applyOptions({ width: chartContainer.value.clientWidth })
    }
  })
  ro.observe(chartContainer.value)
}

onMounted(async () => {
  try {
    const list = await getStrategies()
    if (list?.length) strategies.value = list
  } catch {}
})
</script>

<style scoped>
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
</style>
