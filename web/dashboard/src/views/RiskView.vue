<template>
  <div>
    <el-row :gutter="16" style="margin-bottom: 16px">
      <el-col :span="8">
        <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
          <template #header><span style="color: #e0e0e0">Risk Status</span></template>
          <el-descriptions :column="1" border size="small">
            <el-descriptions-item label="Daily PnL">
              <span :style="{ color: risk.daily_pnl >= 0 ? '#67C23A' : '#F56C6C' }">
                {{ formatPnl(risk.daily_pnl) }} USDT
              </span>
            </el-descriptions-item>
            <el-descriptions-item label="Drawdown">
              {{ formatPercent(-risk.current_drawdown) }}
            </el-descriptions-item>
            <el-descriptions-item label="Peak Equity">
              {{ formatNumber(risk.peak_equity) }} USDT
            </el-descriptions-item>
            <el-descriptions-item label="Current Equity">
              {{ formatNumber(risk.current_equity) }} USDT
            </el-descriptions-item>
            <el-descriptions-item label="Daily Trades">
              {{ risk.daily_trade_count }}
            </el-descriptions-item>
            <el-descriptions-item label="Trading Status">
              <el-tag :type="risk.is_trading_paused ? 'danger' : 'success'" size="small">
                {{ risk.is_trading_paused ? 'PAUSED' : 'ACTIVE' }}
              </el-tag>
            </el-descriptions-item>
            <el-descriptions-item v-if="risk.pause_reason" label="Pause Reason">
              {{ risk.pause_reason }}
            </el-descriptions-item>
          </el-descriptions>
        </el-card>
      </el-col>
      <el-col :span="16">
        <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
          <template #header><span style="color: #e0e0e0">Equity Curve (24h)</span></template>
          <div ref="equityContainer" style="height: 300px"></div>
        </el-card>
      </el-col>
    </el-row>

    <!-- 多仓价格上限 -->
    <el-row :gutter="16">
      <el-col :span="12">
        <el-card shadow="never" style="background: #1d1e1f; border-color: #333">
          <template #header>
            <div style="display: flex; align-items: center; gap: 8px">
              <span style="color: #e0e0e0">多仓价格上限</span>
              <el-tag v-if="limits.max_long_entry_price > 0" type="warning" size="small">
                已启用：≤ {{ formatNumber(limits.max_long_entry_price) }} USDT
              </el-tag>
              <el-tag v-else type="info" size="small">未启用</el-tag>
            </div>
          </template>

          <div style="color: #909399; font-size: 13px; margin-bottom: 16px">
            设置后，当 BTC 价格超过此阈值时，系统将拒绝所有买入开仓信号。设为 0 表示不限制。
          </div>

          <el-form label-width="120px" size="small" @submit.prevent>
            <el-form-item label="价格上限 (USDT)">
              <el-input-number
                v-model="inputPrice"
                :min="0"
                :step="1000"
                :precision="0"
                placeholder="0 = 不限制"
                style="width: 200px"
              />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" :loading="saving" @click="saveLimits">
                保存
              </el-button>
              <el-button v-if="limits.max_long_entry_price > 0" @click="clearLimit">
                清除限制
              </el-button>
            </el-form-item>
          </el-form>

          <el-alert
            v-if="saveMsg"
            :type="saveOk ? 'success' : 'error'"
            :title="saveMsg"
            :closable="false"
            show-icon
            style="margin-top: 8px"
          />
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { createChart, ColorType } from 'lightweight-charts'
import { fetchRiskStatus, fetchRiskLimits, setRiskLimits } from '@/api/risk'
import { fetchSnapshots } from '@/api/snapshots'
import type { RiskStatus, RiskLimits } from '@/types/models'
import { formatNumber, formatPnl, formatPercent } from '@/utils/format'

const risk = ref<RiskStatus>({
  daily_pnl: 0, daily_pnl_pct: 0, current_drawdown: 0, max_drawdown: 0,
  peak_equity: 0, current_equity: 0, daily_trade_count: 0,
  is_trading_paused: false, pause_reason: '', pause_until: '',
})
const limits = ref<RiskLimits>({ max_long_entry_price: 0 })
const inputPrice = ref<number>(0)
const saving = ref(false)
const saveMsg = ref('')
const saveOk = ref(true)
const equityContainer = ref<HTMLElement | null>(null)

onMounted(async () => {
  try { risk.value = await fetchRiskStatus() } catch {}
  try {
    limits.value = await fetchRiskLimits()
    inputPrice.value = limits.value.max_long_entry_price
  } catch {}

  // Equity curve
  if (!equityContainer.value) return
  const chart = createChart(equityContainer.value, {
    layout: { background: { type: ColorType.Solid, color: '#1d1e1f' }, textColor: '#b0b0b0' },
    grid: { vertLines: { color: '#2a2a2a' }, horzLines: { color: '#2a2a2a' } },
    timeScale: { timeVisible: true },
  })
  const series = chart.addLineSeries({ color: '#f0b90b', lineWidth: 2 })

  try {
    const snapshots = await fetchSnapshots({ interval: '5m' })
    if (snapshots && snapshots.length > 0) {
      const data = snapshots.map((s: any) => ({
        time: Math.floor(new Date(s.timestamp ?? s.Timestamp).getTime() / 1000) as any,
        value: s.total_equity ?? s.TotalEquity,
      }))
      series.setData(data)
      chart.timeScale().fitContent()
    }
  } catch {}
})

async function saveLimits() {
  saving.value = true
  saveMsg.value = ''
  try {
    limits.value = await setRiskLimits({ max_long_entry_price: inputPrice.value })
    saveOk.value = true
    saveMsg.value = inputPrice.value > 0
      ? `已设置：BTC 价格超过 ${inputPrice.value.toLocaleString()} USDT 时不开多仓`
      : '已清除价格上限限制'
  } catch (e: any) {
    saveOk.value = false
    saveMsg.value = '保存失败：' + (e?.message ?? '未知错误')
  } finally {
    saving.value = false
  }
}

function clearLimit() {
  inputPrice.value = 0
  saveLimits()
}
</script>
