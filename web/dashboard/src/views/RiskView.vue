<template>
  <div>
    <el-row :gutter="16">
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
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { createChart, ColorType } from 'lightweight-charts'
import { fetchRiskStatus } from '@/api/risk'
import { fetchSnapshots } from '@/api/snapshots'
import type { RiskStatus } from '@/types/models'
import { formatNumber, formatPnl, formatPercent } from '@/utils/format'

const risk = ref<RiskStatus>({
  daily_pnl: 0, daily_pnl_pct: 0, current_drawdown: 0, max_drawdown: 0,
  peak_equity: 0, current_equity: 0, daily_trade_count: 0,
  is_trading_paused: false, pause_reason: '', pause_until: '',
})
const equityContainer = ref<HTMLElement | null>(null)

onMounted(async () => {
  try {
    risk.value = await fetchRiskStatus()
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
</script>
